package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
)

var (
	sep        = flag.String("sep", ",", "separator character (must be one character)")
	lazyQuotes = flag.Bool("lazyquotes", false, "allow lazy quotes: a quote may appear in an unquoted field and a non-doubled quote may appear in a quoted field.")
	outSep     = flag.String("outsep", ",", "separator character on output")
)

func main() {
	flag.Parse()
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: csv [flags] [zero-indexed-field-number ...]\n")
		os.Exit(2)
	}
	sepr := []rune(*sep)
	if len(sepr) != 1 {
		log.Fatalf("must have exactly one character in separator")
	}
	outSepr := []rune(*outSep)
	if len(outSepr) != 1 {
		log.Fatalf("must have exactly one character in output separator")
	}
	args := flag.Args()
	fields := make([]int, len(args))
	for i, arg := range flag.Args() {
		f, err := strconv.Atoi(arg)
		if err != nil {
			log.Fatal(err)
		}
		fields[i] = f
	}

	r := csv.NewReader(os.Stdin)
	r.LazyQuotes = true
	r.Comma = sepr[0]

	w := csv.NewWriter(os.Stdout)
	w.Comma = outSepr[0]

	outRec := make([]string, len(fields))
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			w.Flush()
			log.Fatal(err)
		}
		var out []string
		if len(fields) == 0 {
			out = rec
		} else {
			out = outRec
			for i, n := range fields {
				if n < len(rec) {
					out[i] = rec[n]
				} else {
					out[i] = ""
				}
			}
		}
		if err := w.Write(out); err != nil {
			log.Fatal(err)
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		log.Fatal(err)
	}
}
