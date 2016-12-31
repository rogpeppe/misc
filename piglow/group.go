package piglow

import (
	"fmt"
	"strconv"
	"strings"
)

// Group represents a group of LEDs, such as all the
// LEDs of a particular color.
type Group interface {
	// LEDs returns the set of all the LEDs in the group.
	LEDs() Set
}

// LED represents one of the 18 PiGlow LEDs.
// Values range from 0 to 17 - the device LED
// numbers are one greater than this value
// because they start numbering from 1.
type LED int

// NumLEDs holds the number of LEDs available.
const NumLEDs = 18

// LEDs implements Group.LEDs by returning
// a group with a single member containing the given LED.
func (led LED) LEDs() Set {
	if led < 0 || led >= NumLEDs {
		return 0
	}
	return Set(1 << uint(led))
}

// All defines a Group that holds all the LEDs.
var All = Range{
	R1: NumLEDs,
}

var allSet = All.LEDs()

// Color represents a color of a PiGlow LED.
type Color uint8

// LEDs implements Group.LEDs by returning
// all the LEDs with the given color.
func (col Color) LEDs() Set {
	if int(col) >= len(byColor) {
		return 0
	}
	return byColor[col]
}

// All the colors of the LEDs. Lower colors are closer to the center
// of the spiral.
const (
	White Color = iota
	Blue
	Green
	Yellow
	Orange
	Red
)

var byColor = []Set{
	Red:    SetOf(6, 17, 0),
	Orange: SetOf(7, 16, 1),
	Yellow: SetOf(8, 15, 2),
	Green:  SetOf(5, 13, 3),
	Blue:   SetOf(4, 11, 14),
	White:  SetOf(9, 10, 12),
}

var colorNames = map[string]Color{
	"white":  White,
	"blue":   Blue,
	"green":  Green,
	"orange": Orange,
	"red":    Red,
}

// Set represents a set of LEDs. LED n is represented by the bit 1<<n.
type Set uint32

func (s Set) With(led LED) Set {
	if led >= NumLEDs || led < 0 {
		return s
	}
	return s | Set(1<<uint(led))
}

func (s Set) Without(led LED) Set {
	return s &^ Set(1<<uint(led))
}

func (s Set) Has(led LED) bool {
	return s&Set(1<<uint(led)) != 0
}

// SetOf returns the set of all the given LEDs.
func SetOf(leds ...LED) Set {
	var set Set
	for _, led := range leds {
		set |= led.LEDs()
	}
	return set
}

// Arm represents one of the three arms of the PiGlow
// LED spiral. It ranges from 0 to 2.
type Arm uint8

// NumArms holds the number of LED spiral arms in the PiGlow.
const NumArms = 3

func (a Arm) LEDs() Set {
	if int(a) >= len(byArm) {
		return 0
	}
	return byArm[a]
}

var byArm = []Set{
	SetOf(6, 7, 8, 5, 4, 9),
	SetOf(17, 16, 15, 13, 11, 10),
	SetOf(0, 1, 2, 3, 14, 12),
}

// Range represents a numerical range of LEDs.
type Range struct {
	// R0 holds the start of the range.
	R0 LED
	// R1 holds one beyond the end of the range.
	// If R1 <= R0, the set is empty.
	R1 LED
}

// Range implements Group.LEDs by returning a set
// of all the LEDs in the range.
func (r Range) LEDs() Set {
	r.R0 = constrainLED(r.R0)
	r.R1 = constrainLED(r.R1)
	if r.R1 <= r.R0 {
		return 0
	}
	return (1<<uint(r.R1) - 1) &^ (1<<uint(r.R0) - 1)
}

func constrainLED(led LED) LED {
	if led < 0 {
		return 0
	}
	if led > NumLEDs {
		return NumLEDs
	}
	return led
}

// Radius represents the LEDs a certain distance from
// the center of the spiral. Zero represents the LEDs
// closest to the center; 5 represents the LEDs furthest away.
type Radius uint8

const MaxRadius = 5

func (r Radius) LEDs() Set {
	return Color(r).LEDs()
}

// RadiusRange represents a range of radii.
type RadiusRange struct {
	// R0 holds the start of the range.
	R0 Radius

	// R1 holds one beyond the end of the range.
	// If R1 <= R0, the set is empty.
	R1 Radius
}

// LEDs implements Group.LEDs by returning all the
// LEDs in the given radius range.
func (r RadiusRange) LEDs() Set {
	var set Set
	for i := r.R0; i < r.R1; i++ {
		set |= i.LEDs()
	}
	return set
}

// ParseGroup parses a group from a string. A group
// can be specified in one of the following forms:
//	<number> - the LED with the given decimal number (0-17)
//	<number0>..<number1> - all the LEDs in the half-open range [number0..number1).
//	arm<number> - the arm with the given number (0-3)
//	r<number> - the LEDs the given distance from the center (0-6)
//	r<number0>..r<number0> - all the LEDs in the half-open range of radii [number0..number1)
//	<color-name> - all the LEDs with the given color
//	all			- all the LEDs
func ParseGroup(s string) (Group, error) {
	if col, ok := colorNames[s]; ok {
		return col, nil
	}
	switch {
	case s == "all":
		return All, nil
	case strings.HasPrefix(s, "arm"):
		n, err := strconv.Atoi(s[3:])
		if err != nil || n < 0 || n >= NumArms {
			return nil, fmt.Errorf("invalid arm number in %q", s)
		}
		return Arm(n), nil
	case !strings.HasPrefix(s, "r") && strings.Contains(s, ".."):
		parts := strings.Split(s, "..")
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid range %q", s)
		}
		r0, err0 := strconv.Atoi(parts[0])
		r1, err1 := strconv.Atoi(parts[1])
		if err0 != nil || err1 != nil || r0 < 0 || r1 < 0 || r0 >= NumLEDs || r1 >= NumLEDs || r0 > r1 {
			return nil, fmt.Errorf("invalid numbers in range %q", s)
		}
		return Range{
			R0: LED(r0),
			R1: LED(r1),
		}, nil
	case strings.HasPrefix(s, "r") && strings.Contains(s, ".."):
		parts := strings.Split(s, "..")
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid range %q", s)
		}
		if !strings.HasPrefix(parts[1], "r") {
			return nil, fmt.Errorf("invalid radius range end %q", parts[1])
		}
		r0, err0 := strconv.Atoi(parts[0][1:])
		r1, err1 := strconv.Atoi(parts[1][1:])
		if err0 != nil || err1 != nil || r0 < 0 || r1 < 0 || r0 > MaxRadius || r1 > MaxRadius || r0 > r1 {
			return nil, fmt.Errorf("invalid numbers in radius range %q", s)
		}
		return RadiusRange{
			R0: Radius(r0),
			R1: Radius(r1),
		}, nil
	case strings.HasPrefix(s, "r"):
		n, err := strconv.Atoi(s[1:])
		if err != nil {
			return nil, fmt.Errorf("bad radius %q", s)
		}
		if n < 0 || n > MaxRadius {
			return nil, fmt.Errorf("radius %q out of range", s)
		}
		return Radius(n), nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return nil, fmt.Errorf("unrecognized LED group %q", s)
	}
	if n < 0 || n >= NumLEDs {
		return nil, fmt.Errorf("LED number %q out of range", s)
	}
	return LED(n), nil
}
