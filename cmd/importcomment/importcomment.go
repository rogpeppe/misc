package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/build"
	"go/format"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/kisielk/gotool"
)

var usage = `usage: importpath [-d] [package...]

The importpath command adds an appropriate import
comment to all non-test files in the given repository.

With the -d flag it deletes them.
`

var dflag = flag.Bool("d", false, "delete import comments")

var cwd string

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "%s\n", usage)
		flag.PrintDefaults()
		os.Exit(2)
	}
	flag.Parse()

	var err error
	cwd, err = os.Getwd()
	if err != nil {
		cwd = "."
	}
	pkgs := flag.Args()
	if len(pkgs) == 0 {
		pkgs = []string{"."}
	}
	exitStatus := 0
	pkgs = gotool.ImportPaths(pkgs)
	for _, pkg := range pkgs {
		if err := writeImports(pkg); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v", err)
			exitStatus = 1
		}
	}
	os.Exit(exitStatus)
}

func writeImports(pkgPath string) error {
	pkg, err := build.Import(pkgPath, cwd, build.FindOnly)
	if err != nil {
		warning("cannot read %v: %v", pkgPath, err)
		return nil
	}
	infos, err := ioutil.ReadDir(pkg.Dir)
	if err != nil {
		warning("cannot read %q: %v", pkg.Dir, err)
		return nil
	}
	for _, info := range infos {
		path := filepath.Join(pkg.Dir, info.Name())
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			continue
		}
		data, err := ioutil.ReadFile(path)
		if err != nil {
			warning("cannot read %q: %v", path, err)
			continue
		}
		result := process(data, pkg.ImportPath)
		result, err = format.Source(result)
		if err != nil {
			warning("%s: %v", path, err)
			continue
		}
		if bytes.Equal(result, data) {
			continue
		}
		fmt.Printf("%s\n", path)
		if err := ioutil.WriteFile(path, result, 0666); err != nil {
			warning("cannot write %q: %v", path, err)
		}
	}
	return nil
}

func process(data []byte, pkg string) []byte {
	lines := bytes.Split(data, []byte("\n"))
	for i, line := range lines {
		if !bytes.HasPrefix(line, []byte("package")) {
			continue
		}
		ci := bytes.Index(line, []byte("//"))
		if *dflag {
			if ci != -1 {
				lines[i] = line[0:ci]
			}
			continue
		}
		if ci == -1 {
			ci = len(line)
		}
		lines[i] = []byte(string(line[0:ci]) + fmt.Sprintf(" // import %q", pkg))
		break
	}
	return bytes.Join(lines, []byte("\n"))
}

func warning(f string, a ...interface{}) {
	fmt.Fprintf(os.Stderr, "warning: %s\n", fmt.Sprintf(f, a...))
}
