package piglow_test

import (
	"time"

	"github.com/rogpeppe/misc/piglow"
	"golang.org/x/exp/io/i2c"
)

func Example() {
	p, err := piglow.Open(&i2c.Devfs{Dev: "/dev/i2c-1"})
	if err != nil {
		panic(err)
	}

	if err := p.Setup(); err != nil {
		panic(err)
	}

	brightness := uint8(0)
	allLEDs := piglow.All.LEDs()
	for i := 0; i < 10; i++ {
		brightness ^= 1
		p.SetBrightness(allLEDs, brightness)
		time.Sleep(300 * time.Millisecond)
	}
}
