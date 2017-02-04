// +build amd64,linux

package main

import (
	"syscall"
	"time"
)

func setSysTime(t time.Time) error {
	return syscall.Settimeofday(&syscall.Timeval{
		Sec: int64(t.Unix()),
	})
}
