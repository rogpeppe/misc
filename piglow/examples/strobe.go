package main

import (
	"time"

	"github.com/rogpeppe/misc/piglow"
	"golang.org/x/exp/io/i2c"
)

func main() {
	p, err := piglow.Open(&i2c.Devfs{Dev: "/dev/i2c-1"})
	if err != nil {
		panic(err)
	}
	defer p.Close()

	time.Sleep(50 * time.Millisecond)
	for i := piglow.LED(0); i < piglow.NumLEDs; i++ {
		if err := p.SetBrightness(i.LEDs(), 1); err != nil {
			panic(err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	time.Sleep(50 * time.Millisecond)
	for i := piglow.LED(piglow.NumLEDs - 1); i >= 0; i-- {
		if err := p.SetBrightness(i.LEDs(), 0); err != nil {
			panic(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
