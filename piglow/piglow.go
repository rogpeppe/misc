// Package piglow implements a driver for the Pimoroni PiGlow.
package piglow

import (
	"fmt"

	"golang.org/x/exp/io/i2c"
	"golang.org/x/exp/io/i2c/driver"
)

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

func (p *PiGlow) SetOne(led LED, level uint8) error {
	p.conn.Write([]byte{byte(led + 1), gamma[level]})
	p.conn.Write(sync)
	return nil
}

var sync = []byte{0x16, 0xFF}

// SetBrightness sets the brightness of all the LEDs in the given
// set to the given level.
func (p *PiGlow) SetBrightness(leds Set, level uint8) error {
	if leds == 0 {
		return nil
	}
	buf := make([]byte, 2)
	buf[1] = gamma[level]
	for i := LED(0); i < NumLEDs; i++ {
		if leds&(1<<uint(i)) == 0 {
			continue
		}
		buf[0] = byte(i + 1)
		if err := p.conn.Write(buf); err != nil {
			return err
		}
	}
	return p.conn.Write(sync)
}

// SetAllBrightness sets the brightness of all the LEDs.
// The levels slice holds an element for each LED.
func (p *PiGlow) SetAllBrightness(levels []uint8) error {
	if len(levels) > NumLEDs {
		return fmt.Errorf("too many levels specified")
	}
	for i, level := range levels {
		if err := p.conn.Write([]byte{byte(i + 1), gamma[level]}); err != nil {
			return err
		}
	}
	return p.conn.Write(sync)
}
