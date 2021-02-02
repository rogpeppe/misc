package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"

	"gopkg.in/yaml.v2"
)

func main() {
	var x interface{}
	data, err := ioutil.ReadAll(os.Stdin)
	if err != nil {
		log.Fatal(err)
	}
	if err := yaml.Unmarshal(data, &x); err != nil {
		log.Fatal(err)
	}
	x, err = rewriteMaps("", x)
	if err != nil {
		log.Fatal(err)
	}
	data1, err := json.MarshalIndent(x, "", "  ")
	if err != nil {
		log.Fatal(err)
	}
	os.Stdout.Write(data1)
}

func rewriteMaps(path string, x interface{}) (interface{}, error) {
	switch x := x.(type) {
	case map[interface{}]interface{}:
		nm := make(map[string]interface{})
		for k, v := range x {
			k1, ok := k.(string)
			if !ok {
				return nil, fmt.Errorf("map key at %s.%v (type %T) is not string", path, x, k)
			}
			v1, err := rewriteMaps(path+"."+k1, v)
			if err != nil {
				return nil, err
			}
			nm[k1] = v1
		}
		return nm, nil
	case []interface{}:
		nx := make([]interface{}, len(x))
		for i := range x {
			v, err := rewriteMaps(fmt.Sprintf("path[%d]", i), x[i])
			if err != nil {
				return nil, err
			}
			nx[i] = v
		}
		return nx, nil
	default:
		return x, nil
	}
}
