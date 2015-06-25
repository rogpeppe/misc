package main

var packagetestTemplate = newTemplate(`
// Copyright {{.Year}} Canonical Ltd.

package {{.Name}}cmd_test

import (
	"testing"

	jujutesting "github.com/juju/testing"
)

func TestPackage(t *testing.T) {
	jujutesting.MgoTestPackage(t, nil)
}
`)
