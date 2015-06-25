// The newjujuplugin command generates a skeleton for a multi-command
// juju plugin.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/format"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"
	"unicode"
)

type templateArg struct {
	CmdPackage string
	Name       string
	Year       int
	Commands   []templateOneCmdArg
}

type templateOneCmdArg struct {
	CmdNameLiteral string // e.g. list-something
	CmdName        string // e.g. listSomething
	*templateArg
}

var force = flag.Bool("f", false, "force overwrite of existing source files")

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: newjujuplugin <packagepath>/cmd/juju-<name> [cmd...]\n")
		os.Exit(2)
	}
	flag.Parse()
	if flag.NArg() < 1 {
		flag.Usage()
	}
	cmdPackage := flag.Arg(0)
	lastHyphen := strings.LastIndex(cmdPackage, "-")
	if lastHyphen == -1 {
		fail("package name in wrong form")
	}
	commands := flag.Args()[1:]
	arg := &templateArg{
		CmdPackage: flag.Arg(0),
		Name:       cmdPackage[lastHyphen+1:],
		Year:       time.Now().Year(),
	}
	for _, c := range commands {
		arg.Commands = append(arg.Commands, templateOneCmdArg{
			CmdNameLiteral: c,
			CmdName:        fromLiteral(c),
			templateArg:    arg,
		})
	}
	gopath := os.Getenv("GOPATH")
	if i := strings.Index(gopath, ":"); i > 0 {
		gopath = gopath[0:i]
	}
	dir := filepath.Join(gopath, "src", filepath.FromSlash(cmdPackage))
	writeFile(arg, dir, "main.go", mainTemplate)

	cmdDir := filepath.Join(dir, arg.Name+"cmd")
	writeFile(arg, cmdDir, "cmd.go", cmdTemplate)
	writeFile(arg, cmdDir, "cmd_test.go", cmdtestTemplate)
	writeFile(arg, cmdDir, "package_test.go", packagetestTemplate)

	for _, c := range arg.Commands {
		writeFile(c, cmdDir, c.CmdNameLiteral+".go", onecmdTemplate)
		writeFile(c, cmdDir, c.CmdNameLiteral+"_test.go", onecmdtestTemplate)
	}
	fmt.Println(dir)
}

func fromLiteral(s string) string {
	return toCamelCase(s)
}

var templateFuncs = template.FuncMap{
	"fromLiteral": fromLiteral,
}

func newTemplate(s string) *template.Template {
	return template.Must(template.New("").Funcs(templateFuncs).Parse(strings.TrimLeft(s, "\n")))
}

func writeFile(arg interface{}, dir, file string, template *template.Template) {
	var buf bytes.Buffer
	if err := os.MkdirAll(dir, 0777); err != nil {
		fail("%v", err)
	}
	if err := template.Execute(&buf, arg); err != nil {
		fail("cannot execute template for %s: %v", file, err)
	}
	path := filepath.Join(dir, file)
	data, err := format.Source(buf.Bytes())
	if err != nil {
		fail("invalid source generated for %s: %v", path, err)
	}
	if _, err := os.Stat(path); err == nil && !*force {
		fmt.Printf("not overwriting %s\n", path)
		return
	}
	if err := ioutil.WriteFile(path, data, 0777); err != nil {
		fail("%v", err)
	}
}

func fail(f string, a ...interface{}) {
	fmt.Fprintf(os.Stderr, "%s\n", fmt.Sprintf(f, a...))
	os.Exit(1)
}

func toCamelCase(s string) (r string) {
	if s == "" {
		return s
	}
	wasHyphen := false
	var out []rune
	for _, r := range s {
		if r == '-' {
			wasHyphen = true
			continue
		}
		if wasHyphen {
			r = unicode.ToUpper(r)
		}
		out = append(out, r)
		wasHyphen = false
	}
	return string(out)
}
