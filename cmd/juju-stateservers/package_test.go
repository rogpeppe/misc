package main

import (
	"testing"

	coretesting "github.com/juju/juju/testing"
)

func TestPackage(t *testing.T) {
	coretesting.MgoTestPackage(t)
}
