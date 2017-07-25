package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/rogpeppe/misc/ds1307"
	"golang.org/x/exp/io/i2c"
)

var setSys = flag.Bool("sys", false, "use RTC time to set system clock")

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: rtc [flags] [yyyy-mmddThh:mm:ssZ]\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "If the time argument is specified, the RTC time will be set\n")
	}
	flag.Parse()
	rtc, err := ds1307.Open(&i2c.Devfs{Dev: "/dev/i2c-1"})
	if err != nil {
		log.Fatalf("cannot open: %v", err)
	}
	if flag.NArg() > 0 {
		if err := setTime(rtc, flag.Arg(0)); err != nil {
			log.Fatalf("cannot set time: %v", err)
		}
		if !*setSys {
			return
		}
	}
	t, err := rtc.Now()
	if err != nil {
		log.Fatalf("cannot get now: %v", err)
	}
	if *setSys {
		if err := setSysTime(t); err != nil {
			log.Fatalf("cannot set system time: %v", err)
		}
		return
	}
	fmt.Println(t)
}

func setTime(rtc *ds1307.RTC, tstr string) error {
	t, err := time.Parse(time.RFC3339, tstr)
	if err != nil {
		return err
	}
	return rtc.Set(t)
}
