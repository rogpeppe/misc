package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/juju/ratelimit"
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `
usage: slow bytes-per-second [buffer-size / 1024]

The slow command reads from standard input and writes to
standard output, limiting throughput to the given number
of bytes per second
`[1:])
		os.Exit(1)
	}
	flag.Parse()
	if flag.NArg() < 1 {
		flag.Usage()
	}
	rate, err := strconv.ParseFloat(flag.Arg(0), 64)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bad bytes-per-second argument\n")
		flag.Usage()
	}
	bufSize := int64(64 * 1024)
	if sizeStr := flag.Arg(1); sizeStr != "" {
		fmt.Fprintf(os.Stderr, "bad buffer-size argument\n")
		bufSize, err = strconv.ParseInt(flag.Arg(1), 10, 64)
		if err != nil {
			flag.Usage()
		}
		bufSize *= 1024
	}

	b := ratelimit.NewBucketWithRate(rate, bufSize)
	buf := make([]byte, 1024)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			return
		}
		b.Wait(int64(n))
		if _, err := os.Stdout.Write(buf[0:n]); err != nil {
			log.Fatal("write error: %v", err)
		}
	}
}
