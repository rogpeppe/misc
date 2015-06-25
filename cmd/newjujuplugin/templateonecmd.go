package main

var onecmdTemplate = newTemplate(`
// Copyright {{.Year}} Canonical Ltd.

package {{.Name}}cmd

import (
	"github.com/juju/cmd"
	"launchpad.net/gnuflag"
)

type {{.CmdName}}Command struct {
	cmd.CommandBase
}

var {{.CmdName}}Doc = ` + "`" + `
The {{.CmdNameLiteral}} command ... TODO.
` + "`" + `

func (c *{{.CmdName}}Command) Info() *cmd.Info {
	return &cmd.Info{
		Name:    {{printf "%q" .CmdNameLiteral}},
		Args:    "TODO",
		Purpose: "TODO",
		Doc:     {{.CmdName}}Doc,
	}
}

func (c *{{.CmdName}}Command) SetFlags(f *gnuflag.FlagSet) {
	// f.StringVar(&c.value, "flagname", "default", "description")
}

func (c *{{.CmdName}}Command) Init(args []string) error {
	// TODO
	return nil
}

func (c *{{.CmdName}}Command) Run(ctxt *cmd.Context) error {
	// TODO
	return nil
}
`)
