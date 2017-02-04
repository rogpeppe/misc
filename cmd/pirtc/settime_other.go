// +build !linux linux,!amd64,!arm

package main

import "errors"

func setSysTime(t time.Time) error {
	return errors.New("cannot set system time on this os/arch configuration")
}
