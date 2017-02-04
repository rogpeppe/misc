// +build arm,linux

package main

import (
	"syscall"
	"time"
)

func setSysTime(t time.Time) error {
	return syscall.Settimeofday(&syscall.Timeval{
		Sec: int32(t.Unix()),
	})
}
