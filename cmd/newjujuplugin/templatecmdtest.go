package main

var cmdtestTemplate = newTemplate(`
// Copyright {{.Year}} Canonical Ltd.

package {{.Name}}cmd_test

import (
	"bytes"
	"strings"

	"github.com/juju/cmd"
	"github.com/juju/testing"
	gc "gopkg.in/check.v1"
	"launchpad.net/loggo"

	{{printf "%q" (printf "%s/%scmd" .CmdPackage .Name)}}
)

// run runs a {{.Name}} plugin subcommand with the given arguments,
// its context directory set to dir. It returns the output of the command
// and its exit code.
func run(dir string, args ...string) (stdout, stderr string, exitCode int) {
	// Remove the warning writer usually registered by cmd.Log.Start, so that
	// it is possible to run multiple commands in the same test.
	// We are not interested in possible errors here.
	defer loggo.RemoveWriter("warning")
	var stdoutBuf, stderrBuf bytes.Buffer
	ctxt := &cmd.Context{
		Dir:    dir,
		Stdin:  strings.NewReader(""),
		Stdout: &stdoutBuf,
		Stderr: &stderrBuf,
	}
	exitCode = cmd.Main({{.Name}}cmd.New(), ctxt, args)
	return stdoutBuf.String(), stderrBuf.String(), exitCode
}

type commonSuite struct {
	testing.IsolatedMgoSuite
}

func (s *commonSuite) SetUpTest(c *gc.C) {
	s.IsolatedMgoSuite.SetUpTest(c)
	// TODO delete this method if there's nothing else here.
}

func (s *commonSuite) TearDownTest(c *gc.C) {
	// TODO delete this method if there's nothing else here.
	s.IsolatedMgoSuite.TearDownTest(c)
}
`)
