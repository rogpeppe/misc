package main

import (
	"testing"

	qt "github.com/frankban/quicktest"
	yaml "gopkg.in/yaml.v1"
)

var parseTests = []struct {
	testName    string
	data        string
	expect      string
	expectError string
}{{
	testName: "single-schema",
	data: `schema AccountContractInfo {
	"properties": {
		"accountInfo": {
			"$ref": "#/components/schemas/AccountInfo"
		},
		"contractInfo": {
			"$ref": "#/components/schemas/ContractInfo"
		}
	},
	"type": "object"
}`,
	expect: `
components:
  schemas:
    AccountContractInfo:
      type: object
      properties:
        accountInfo:
          $ref: "#/components/schemas/AccountInfo"
        contractInfo:
          $ref: "#/components/schemas/ContractInfo"
`,
}}

func TestParse(t *testing.T) {
	c := qt.New(t)
	for _, test := range parseTests {
		c.Run(test.testName, func(c *qt.C) {
			var spec openAPISpec
			err := spec.parse("somefile", []byte(test.data))
			if test.expectError != "" {
				c.Assert(err, qt.ErrorMatches, test.expectError)
				return
			}
			c.Assert(err, qt.Equals, nil)
			c.Logf("spec: %#v", spec)
			var want interface{}
			err = yaml.Unmarshal([]byte(test.expect), &want)
			c.Assert(err, qt.Equals, nil)
			var got interface{}
			gotData, err := yaml.Marshal(spec)
			c.Assert(err, qt.Equals, nil)
			err = yaml.Unmarshal([]byte(gotData), &got)
			c.Assert(err, qt.Equals, nil)
			c.Assert(got, qt.DeepEquals, want)
		})
	}
}
