// The jujuapidoc command generates a JSON file containing
// details of as many Juju RPC calls as it can get its hands on.
//
// It depends on a custom addition to the apiserver package,
// FacadeRegistry.ListDetails, the implementation of which
// can be found in https://github.com/rogpeppe/juju/tree/076-apiserver-facade-list-details.
//
// The resulting JSON output can be processed into HTML by
// the jujuapidochtml command.
package main

import (
	"bytes"
	"flag"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/errgo.v1"
)

//go:generate go-bindata jujugenerateapidoc

func main() {
	flag.Parse()

	if err := runMain(); err != nil {
		log.Fatal(err)
	}
}

func runMain() error {
	dir, err := ioutil.TempDir("", "")
	if err != nil {
		return errgo.Mask(err)
	}
	defer os.RemoveAll(dir)
	progData, err := Asset("jujugenerateapidoc/prog.go")
	if err != nil {
		return errgo.Mask(err)
	}
	generatePath := filepath.Join(dir, "generate.go")
	if err := ioutil.WriteFile(generatePath, []byte(progData), 0666); err != nil {
		return errgo.Mask(err)
	}

	var importBuf bytes.Buffer
	cmd := exec.Command("go", "list", "-f", `{{join .Imports "\n"}}`, generatePath)
	cmd.Stderr = os.Stderr
	cmd.Stdout = &importBuf
	if err := cmd.Run(); err != nil {
		return errgo.Notef(err, "cannot get imports from %q", generatePath)
	}
	allImports := strings.Fields(importBuf.String())
	cmd = exec.Command("go", append([]string{"install"}, allImports...)...)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stderr
	if err := cmd.Run(); err != nil {
		return errgo.Notef(err, "cannot install deps")
	}
	cmd = exec.Command("go", "build", generatePath)
	cmd.Dir = dir
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stderr
	if err := cmd.Run(); err != nil {
		return errgo.Notef(err, "cannot build doc generator program")
	}
	cmd = exec.Command(filepath.Join(dir, "generate"))
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	if err := cmd.Run(); err != nil {
		return errgo.Notef(err, "generate info failed")
	}
	return nil
}
