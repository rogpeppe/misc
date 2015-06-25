package main

var onecmdtestTemplate = newTemplate(`
// Copyright {{.Year}} Canonical Ltd.

package {{.Name}}cmd_test


import (
	gc "gopkg.in/check.v1"
)

type {{.CmdName}}Suite struct {
	commonSuite
}

var _ = gc.Suite(&{{.CmdName}}Suite{})

// TODO tests
`)
