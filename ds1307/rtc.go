// Package ds1307 provides support for the DS1307 device as found in the
// RTC Pi Plus (https://www.abelectronics.co.uk/p/52/RTC-Pi-Plus).
package ds1307

import (
	"errors"
	"fmt"
	"time"

	"golang.org/x/exp/io/i2c"
	"golang.org/x/exp/io/i2c/driver"
)

// addr is the I2C address of the device.
const addr = 0x68

// RTC represents a DS1307 real time clock.
type RTC struct {
	conn *i2c.Device
}

// initialConfig holds the initial configuration - square wave and output disabled, frequency set
// to 32.768KHz.
const initialConfig = 0x3

// century holds the current century - the DS1307 does not store the
// current century so that has to be added on manually. We don't support
// setting the current time to any date outside the 21st century.
const century = 2000

type register uint8

const (
	rSecond  register = 0x00
	rMinute  register = 0x01
	rHour    register = 0x02
	rWeekday register = 0x03
	rDay     register = 0x04
	rMonth   register = 0x05
	rYear    register = 0x06
	rControl register = 0x07
)

// Open opens a new RTC. An RTC must be closed when no longer used.
func Open(o driver.Opener) (*RTC, error) {
	c, err := i2c.Open(o, addr)
	if err != nil {
		return nil, err
	}
	rtc := &RTC{conn: c}
	if err := rtc.setup(); err != nil {
		rtc.Close()
		return nil, fmt.Errorf("cannot set up: %v", err)
	}
	return rtc, nil
}

// Close frees the underlying resources. It must be called once
// the RTC is no longer in use.
func (c *RTC) Close() error {
	return c.conn.Close()
}

// setup sets up the initial configuration - square wave and output disabled, frequency set
// to 32.768KHz.
func (c *RTC) setup() error {
	// TODO define bits for other possibilities; see https://datasheets.maximintegrated.com/en/ds/DS1307.pdf
	return c.writeReg(rControl, []byte{0x3})
}

// Now returns the current time, accurate to the nearest second.
func (c *RTC) Now() (time.Time, error) {
	var buf [rYear + 1]byte
	if err := c.readReg(rSecond, buf[:]); err != nil {
		return time.Time{}, err
	}
	year := bcdToDec(buf[rYear]) + century
	month := time.Month(bcdToDec(buf[rMonth]))
	day := bcdToDec(buf[rDay])
	hour := bcdToDec(buf[rHour])
	min := bcdToDec(buf[rMinute])
	sec := bcdToDec(buf[rSecond])
	return time.Date(year, month, day, hour, min, sec, 0, time.UTC), nil
}

var errYearOutOfRange = errors.New("year out of range")

// Set sets the current time. It returns an error if the time
// is not within the 21st century.
func (c *RTC) Set(t time.Time) error {
	t = t.UTC()
	// Round to the nearest second.
	if t.Nanosecond() >= 0.5e9 {
		t = t.Add(time.Second)
	}
	if t.Year() < century || t.Year() >= century+100 {
		return errYearOutOfRange
	}
	buf := [rYear + 1]byte{
		rWeekday: decToBCD(int(t.Weekday() + 1)),
		rYear:    decToBCD(t.Year() - century),
		rMonth:   decToBCD(int(t.Month())),
		rDay:     decToBCD(t.Day()),
		rHour:    decToBCD(t.Hour()),
		rMinute:  decToBCD(t.Minute()),
		rSecond:  decToBCD(t.Second()),
	}
	if err := c.writeReg(rSecond, buf[:]); err != nil {
		return err
	}
	return nil
}

func (c *RTC) writeReg(r register, buf []byte) error {
	return c.conn.WriteReg(byte(r), buf)
}

func (c *RTC) readReg(r register, buf []byte) error {
	return c.conn.ReadReg(byte(r), buf)
}

func bcdToDec(x byte) int {
	return int(x) - 6*(int(x)>>4)
}

func decToBCD(x int) byte {
	return byte((x / 10 * 16) + (x % 10))
}
