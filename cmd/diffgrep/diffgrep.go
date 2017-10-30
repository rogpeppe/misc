package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"

	"sourcegraph.com/sourcegraph/go-diff/diff"
)

func usage() {
	fmt.Fprintf(os.Stderr, "usage: diffgrep regex [flags]\n")
	flag.PrintDefaults()
	os.Exit(2)
}

var (
	fflag   = flag.Bool("f", false, "grep file names instead of content")
	vflag   = flag.Bool("v", false, "invert results")
	dflag   = flag.Bool("d", false, "search deleted content")
	iflag   = flag.Bool("i", false, "search inserted content")
	aflag   = flag.Bool("a", false, "search context too")
	lflag   = flag.Bool("l", false, "include whole files if they contain a match")
	Lflag   = flag.Bool("L", false, "exclude whole files if they contain a match")
	pattern *regexp.Regexp
)

func main() {
	flag.Usage = usage
	flag.Parse()
	if flag.NArg() != 1 {
		flag.Usage()
	}
	if *lflag && *Lflag {
		fmt.Fprintf(os.Stderr, "flag -l conflicts with -L flag")
		flag.Usage()
	}
	if !*iflag && !*dflag && !*fflag {
		*iflag = true
		*dflag = true
	}
	pat, err := regexp.Compile(flag.Arg(0))
	if err != nil {
		fmt.Fprintf(os.Stderr, "bad pattern: %v", err)
		os.Exit(2)
	}
	pattern = pat
	r := diff.NewMultiFileDiffReader(os.Stdin)
	for {
		fdiff, err := r.ReadFile()
		if err != nil {
			if err == io.EOF {
				break
			}
			fmt.Fprintf(os.Stderr, "error reading diff: %v", err)
			break
		}
		fdiff = process(fdiff)
		if fdiff == nil {
			continue
		}
		data, err := diff.PrintFileDiff(fdiff)
		if err != nil {
			fmt.Fprintf(os.Stderr, "cannot print diff: %v", err)
		}
		os.Stdout.Write(data)
	}
}

func process(d *diff.FileDiff) *diff.FileDiff {
	if *fflag {
		match := pattern.MatchString(d.OrigName) || pattern.MatchString(d.NewName)
		if match != *vflag {
			return d
		}
		return nil
	}
	if *lflag || *Lflag {
		for _, h := range d.Hunks {
			matched := matchHunk(h) != *vflag
			if matched {
				if *lflag {
					return d
				}
				return nil
			}
		}
		if *lflag {
			return nil
		}
		return d
	}
	newHunks := make([]*diff.Hunk, 0, len(d.Hunks))
	for _, h := range d.Hunks {
		if matchHunk(h) != *vflag {
			newHunks = append(newHunks, h)
		}
	}
	d.Hunks = newHunks
	return d
}

func matchHunk(h *diff.Hunk) bool {
	scan := bufio.NewScanner(bytes.NewReader(h.Body))
	scan.Buffer(make([]byte, len(h.Body)), 0)
	for scan.Scan() {
		matched := false
		line := scan.Bytes()
		switch line[0] {
		case '+':
			matched = *iflag && pattern.Match(line[1:])
		case '-':
			matched = *dflag && pattern.Match(line[1:])
		case ' ':
			matched = *aflag && pattern.Match(line[1:])
		}
		if matched {
			return true
		}
	}
	return false
}
