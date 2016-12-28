// Package piglow implements a driver for the Pimoroni PiGlow.
package piglow

import (
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/exp/io/i2c"
	"golang.org/x/exp/io/i2c/driver"
)

// LED represents one of the 18 PiGlow LEDs.
// Values range from 0 to 17. The LED numbers
// are not the same as those in the device itself - instead
// they are numbered by position, with each
// range of 6 numbers representing the LEDs in
// an arm of the spiral.
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
	Red:    SetOf(0, 6, 12),
	Orange: SetOf(1, 7, 13),
	Yellow: SetOf(2, 8, 14),
	Green:  SetOf(3, 9, 15),
	Blue:   SetOf(4, 10, 16),
	White:  SetOf(5, 11, 17),
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
	Range{0, 6}.LEDs(),
	Range{6, 12}.LEDs(),
	Range{12, 18}.LEDs(),
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
	if r.R1 <= r.R0 || r.R0 < 0 || r.R0 > NumLEDs || r.R1 < 0 || r.R1 > NumLEDs {
		return 0
	}
	return (1<<uint(r.R1) - 1) &^ (1<<uint(r.R0) - 1)
}

// Radius represents the LEDs a certain distance from
// the center of the spiral. Zero represents the LEDs
// closest to the center; 6 represents the LEDs furthest away.
type Radius uint8

const MaxRadius = 6

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

// Group represents a group of LEDs, such as all the
// LEDs of a particular color.
type Group interface {
	// LEDs returns the set of all the LEDs in the group.
	LEDs() Set
}

// gamma holds the gamma correction table
// for the LED brightness.
// Stolen from github.com/benleb/PyGlow.
var gamma = [256]byte{
	0, 1, 1, 1, 1, 1, 1, 1,
	1, 1, 1, 1, 1, 1, 1, 1,
	1, 1, 1, 1, 1, 1, 1, 1,
	1, 1, 1, 1, 1, 1, 1, 1,
	2, 2, 2, 2, 2, 2, 2, 2,
	2, 2, 2, 2, 2, 2, 2, 2,
	2, 2, 2, 3, 3, 3, 3, 3,
	3, 3, 3, 3, 3, 3, 3, 3,
	4, 4, 4, 4, 4, 4, 4, 4,
	4, 4, 4, 5, 5, 5, 5, 5,
	5, 5, 5, 6, 6, 6, 6, 6,
	6, 6, 7, 7, 7, 7, 7, 7,
	8, 8, 8, 8, 8, 8, 9, 9,
	9, 9, 10, 10, 10, 10, 10, 11,
	11, 11, 11, 12, 12, 12, 13, 13,
	13, 13, 14, 14, 14, 15, 15, 15,
	16, 16, 16, 17, 17, 18, 18, 18,
	19, 19, 20, 20, 20, 21, 21, 22,
	22, 23, 23, 24, 24, 25, 26, 26,
	27, 27, 28, 29, 29, 30, 31, 31,
	32, 33, 33, 34, 35, 36, 36, 37,
	38, 39, 40, 41, 42, 42, 43, 44,
	45, 46, 47, 48, 50, 51, 52, 53,
	54, 55, 57, 58, 59, 60, 62, 63,
	64, 66, 67, 69, 70, 72, 74, 75,
	77, 79, 80, 82, 84, 86, 88, 90,
	91, 94, 96, 98, 100, 102, 104, 107,
	109, 111, 114, 116, 119, 122, 124, 127,
	130, 133, 136, 139, 142, 145, 148, 151,
	155, 158, 161, 165, 169, 172, 176, 180,
	184, 188, 192, 196, 201, 205, 210, 214,
	219, 224, 229, 234, 239, 244, 250, 255,
}

const addr = 0x54 // address is the I2C address of the device.

// PiGlow represents a PiGlow device
type PiGlow struct {
	conn *i2c.Device
}

// Reset resets the internal registers
func (p *PiGlow) Reset() error {
	return p.conn.Write([]byte{0x17, 0xFF})
}

// Shutdown sets the software shutdown mode of the PiGlow
func (p *PiGlow) Shutdown() error {
	return p.conn.Write([]byte{0x00, 0x00})
}

// Enable enables the PiGlow for normal operations
func (p *PiGlow) Enable() error {
	return p.conn.Write([]byte{0x00, 0x01})
}

// Setup enables normal operations, resets the internal registers, and enables
// all LED control registers
func (p *PiGlow) Setup() error {
	if err := p.Reset(); err != nil {
		return err
	}
	if err := p.Enable(); err != nil {
		return err
	}
	if err := p.SetLEDControlRegister(1, 0xFF); err != nil {
		return err
	}
	if err := p.SetLEDControlRegister(2, 0xFF); err != nil {
		return err
	}
	if err := p.SetLEDControlRegister(3, 0xFF); err != nil {
		return err
	}
	return nil
}

// Open opens a new PiGlow. A PiGlow must be closed if no longer in use.
// If the PiGlow has not been powered down since last use, it will be opened
// with its last programmed state.
func Open(o driver.Opener) (*PiGlow, error) {
	conn, err := i2c.Open(o, addr)
	if err != nil {
		return nil, err
	}
	return &PiGlow{conn: conn}, nil
}

// Close frees the underlying resources. It must be called once
// the PiGlow is no longer in use.
func (p *PiGlow) Close() error {
	return p.conn.Close()
}

// SetLEDControlRegister sets the control register 1-3 to the bitmask enables.
//   bitmask definition:
//   0 - LED disabled
//   1 - LED enabled
//   LED Control Register 1 - LED channel 1  to 6   bits 0-5
//   LED Control Register 2 - LED channel 7  to 12  bits 0-5
//   LED Control Register 3 - LED channel 13 to 18  bits 0-5
func (p *PiGlow) SetLEDControlRegister(register, enables int) error {
	var address byte

	switch register {
	case 1:
		address = 0x13
	case 2:
		address = 0x14
	case 3:
		address = 0x15
	default:
		return fmt.Errorf("%d is an unknown register", register)
	}

	if err := p.conn.Write([]byte{address, byte(enables)}); err != nil {
		return err
	}
	return p.conn.Write([]byte{0x16, 0xFF})
}

var ledHex = [18]byte{
	0x07, 0x08, 0x09, 0x06, 0x05, 0x0A,
	0x12, 0x11, 0x10, 0x0E, 0x0C, 0x0B,
	0x01, 0x02, 0x03, 0x04, 0x0F, 0x0D,
}

// SetBrightness sets the brightness of all the LEDs in the given
// set to the given level.
func (p *PiGlow) SetBrightness(leds Set, level uint8) error {
	for i := LED(0); i < NumLEDs; i++ {
		if leds&(1<<uint(i)) == 0 {
			continue
		}
		if err := p.conn.Write([]byte{ledHex[i], gamma[level]}); err != nil {
			return err
		}
	}
	return p.conn.Write([]byte{0x16, 0xFF})
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
