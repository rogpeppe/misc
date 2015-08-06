package main

var mainTemplate = newTemplate(`
// Copyright 2015 Canonical Ltd.
package main

import (
	"fmt"
	"os"

	"github.com/juju/cmd"
	"github.com/juju/juju/juju"

	{{printf "%q" (printf "%s/%scmd" .CmdPackage .Name)}}
)

func main() {
	if err := juju.InitJujuHome(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}
	ctxt := &cmd.Context{
		Dir:    ".",
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Stdin:  os.Stdin,
	}
	os.Exit(cmd.Main({{.Name}}cmd.New(), ctxt, os.Args[1:]))
}
`)
