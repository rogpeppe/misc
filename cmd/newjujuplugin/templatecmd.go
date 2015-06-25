package main

var cmdTemplate = newTemplate(`
// Copyright {{.Year}} Canonical Ltd.

package {{.Name}}cmd

import (
	"os"

	"github.com/juju/cmd"
)

// jujuLoggingConfigEnvKey matches osenv.JujuLoggingConfigEnvKey
// in the Juju project.
const jujuLoggingConfigEnvKey = "JUJU_LOGGING_CONFIG"

var cmdDoc = ` + "`" + `
The juju {{.Name}} command provides ... TODO.
` + "`" + `

// New returns a command that can execute juju-{{.Name}}
// commands.
func New() cmd.Command {
	supercmd := cmd.NewSuperCommand(cmd.SuperCommandParams{
		Name:        {{printf "%q" .Name}},
		UsagePrefix: "juju",
		Doc:         cmdDoc,
		Purpose:     "TODO",
		Log: &cmd.Log{
			DefaultConfig: os.Getenv(jujuLoggingConfigEnvKey),
		},
	})
	{{range .Commands}}supercmd.Register(&{{.CmdName}}Command{})
{{end}}
	return supercmd
}
`)
