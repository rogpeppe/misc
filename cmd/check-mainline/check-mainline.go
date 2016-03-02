package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"go/build"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"

	"github.com/juju/utils/parallel"

	"gopkg.in/errgo.v1"
)

var exitStatus = 0

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: check-mainline package...\n")
		fmt.Fprintf(os.Stderr, `
This command checks that all dependencies of the named
packages are at a commit that is included in the origin remote.

Non-git repositories are ignored (with a warning).
`)
		os.Exit(2)
	}
	flag.Parse()
	pkgs := flag.Args()
	cmd := exec.Command("godeps", append([]string{"-t"}, pkgs...)...)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		log.Fatal(err)
	}
	infos, err := parseDeps(bytes.NewReader(out))
	if err != nil {
		log.Fatal(err)
	}
	run := parallel.NewRun(10)
	for _, info := range infos {
		info := info
		if info.vcs != "git" {
			warningf("ignoring %s repo: %s", info.vcs, info.project)
			continue
		}
		pkg, _ := build.Import(info.project, "", build.FindOnly)
		if pkg.Dir == "" {
			warningf("cannot find %s", info.project)
			exitStatus = 1
			continue
		}
		run.Do(func() error {
			ok, err := inMainline(pkg.Dir, info.revid)
			if err != nil {
				return errgo.Notef(err, "warning: cannot determine mainline status for %s", info.project)
			}
			if !ok {
				return errgo.Newf("%s is not mainline", info.project)
			}
			return nil
		})
	}
	if err := run.Wait(); err != nil {
		for _, e := range err.(parallel.Errors) {
			fmt.Fprintln(os.Stderr, e)
		}
		os.Exit(1)
	}
}

func inMainline(dir string, revid string) (bool, error) {
	cmd := exec.Command("git", "-C", dir, "branch", "-a", "--contains", revid)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return false, errgo.Newf("branch command failed: %v", err)
	}
	for _, f := range strings.Fields(string(out)) {
		if strings.HasPrefix(f, "remotes/origin/") {
			return true, nil
		}
	}
	return false, nil
}

func parseDeps(r io.Reader) ([]*depInfo, error) {
	var deps []*depInfo
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		info, err := parseDepInfo(line)
		if err != nil {
			return nil, fmt.Errorf("cannot parse %q: %v", line, err)
		}
		deps = append(deps, info)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read error: %v", err)
	}
	return deps, nil
}

func (info *depInfo) String() string {
	return fmt.Sprintf("%s\t%s\t%s\t%s", info.project, info.vcs, info.revid, info.revno)
}

type depInfo struct {
	project, vcs, revid, revno string
}

// parseDepInfo parses a dependency info line as printed by
// depInfo.String.
func parseDepInfo(s string) (*depInfo, error) {
	fields := strings.Split(s, "\t")
	if len(fields) != 4 {
		return nil, fmt.Errorf("expected 4 tab-separated fields, got %d", len(fields))
	}
	info := &depInfo{
		project: fields[0],
		vcs:     fields[1],
		revid:   fields[2],
		revno:   fields[3],
	}
	if info.vcs == "" {
		return nil, fmt.Errorf("unknown VCS kind %q", fields[1])
	}
	if info.project == "" {
		return nil, fmt.Errorf("empty project field")
	}
	if info.revid == "" {
		return nil, fmt.Errorf("empty revision id")
	}
	return info, nil
}

func warningf(f string, a ...interface{}) {
	fmt.Fprintf(os.Stderr, "warning: %s\n", fmt.Sprintf(f, a...))
}
