package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"

	yaml "gopkg.in/yaml.v1"
)

func main() {
	data, err := ioutil.ReadAll(os.Stdin)
	if err != nil {
		log.Fatal(err)
	}
	var spec openAPISpec
	if err := spec.parse(data); err != nil {
		log.Fatal(err)
	}
	spec.Version = "3.0.0"
	data, err = yaml.Marshal(spec)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%s", data)
}
