package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"

	yaml "gopkg.in/yaml.v1"
)

func main() {
	log.SetFlags(0)
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: openapi file...\n")
		os.Exit(2)
	}
	flag.Parse()
	if flag.NArg() < 1 {
		flag.Usage()
	}
	var spec openAPISpec
	spec.Version = "3.0.0"
	for _, filename := range flag.Args() {
		data, err := ioutil.ReadFile(filename)
		if err != nil {
			log.Fatal(err)
		}
		if err := spec.parse(filename, data); err != nil {
			log.Fatal(err)
		}
	}
	data, err := yaml.Marshal(spec)
	if err != nil {
		log.Fatal(err)
	}
	os.Stdout.Write(data)
}
