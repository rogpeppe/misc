package main

var mainTemplate = newTemplate(`
// Copyright 2015 Canonical Ltd.
package main

import (
	"os"

	"github.com/juju/cmd"

	{{printf "%q" (printf "%s/%scmd" .CmdPackage .Name)}}
)

func main() {
	ctxt := &cmd.Context{
		Dir:    ".",
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Stdin:  os.Stdin,
	}
	os.Exit(cmd.Main({{.Name}}cmd.New(), ctxt, os.Args[1:]))
}
`)
