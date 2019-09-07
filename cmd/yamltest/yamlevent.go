package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"

	"github.com/google/go-cmp/cmp"
	"github.com/kr/pretty"
	errgo "gopkg.in/errgo.v1"
	yaml "gopkg.in/yaml.v2"
)

var generate = flag.Bool("generate", false, "generate in.json files")
var tests = flag.Bool("tests", false, "generate YAML unmarshal tests from test failures")

type unmarshalTest struct {
	Comment string
	Data    string
	Value   interface{}
}

var allTests []unmarshalTest

func main() {
	flag.Parse()
	for _, f := range flag.Args() {
		if err := check(f); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %s\n", f, err)
		}
	}
	if *tests && len(allTests) > 0 {
		if err := testTemplate.Execute(os.Stdout, allTests); err != nil {
			log.Fatal(err)
		}
	}
}

var testTemplate = template.Must(template.New("").Funcs(template.FuncMap{
	"pretty": func(x interface{}) string {
		return fmt.Sprintf("% #v", pretty.Formatter(x))
	},
}).Parse(`
var unmarshalTests = []struct {
	data string
	value interface{}
}{
	{{range .}}{{if .Comment}}// {{.Comment}}
{{end}}{
		{{.Data | printf "%q"}},
		{{.Value | pretty}},
	},
{{end}}
}
`))

func check(path string) error {
	defer func() {
		if err := recover(); err != nil {
			log.Printf("panic parsing %s", path)
			panic(err)
		}
	}()
	tmlData, err := ioutil.ReadFile(path)
	if err != nil {
		return err
	}
	parts := strings.Split(string(tmlData), "\n+++ ")
	sections := make(map[string]string)
	for _, part := range parts[1:] {
		i := strings.Index(part, "\n")
		if i == -1 {
			return errgo.Newf("invalid part %q", part)
		}
		sections[part[:i]] = part[i+1:]
	}
	sections["header"] = parts[0]
	_, expectError := sections["error"]
	v, err := valueFromEvents(strings.NewReader(unquoteSection(sections["test-event"])))
	if err != nil {
		if expectError {
			return nil
		}
		return errgo.Notef(err, "cannot make object")
	}
	if expectError {
		return errgo.Newf("expected error but got success")
	}
	if *generate {
		return generateJSON(path, v, sections)
	}
	// go-yaml can't currently read multiple documents,
	// so check only the first one.
	v1 := v.([]interface{})
	if len(v1) == 0 {
		v = nil
	} else {
		v = v1[0]
	}
	inYAML := unquoteSection(sections["in-yaml"])
	if err := checkYAML(path, inYAML, v); err == nil || !*tests {
		return errgo.Mask(err)
	}
	allTests = append(allTests, unmarshalTest{
		Comment: headerComment(path, sections["header"]),
		Data:    inYAML,
		Value:   v,
	})
	return nil
}

func headerComment(path, s string) string {
	if t := strings.TrimPrefix(s, "=== "); len(t) == len(s) {
		return ""
	} else {
		s = t
	}
	if i := strings.Index(s, "\n"); i > 0 {
		s = s[:i]
	}
	testId := strings.TrimSuffix(filepath.Base(path), ".tml")
	if testId != "" {
		s = "yaml-test-suite " + testId + ": " + s
	}
	return s
}

var replacements = []struct {
	pat, repl string
}{
	{`^#.*\n`, ``},
	{`^yy .*`, ``},
	{`^%\w.*`, ``},
	{`^[\ \t]*$`, ``},
	{`<SPC>`, ` `},
	{`<TAB>`, "\t"},
	{`^\\`, ``},
}

func unquoteSection(s string) string {
	// Rules taken from yaml-test-suite/bin/generate.pm
	for _, rule := range replacements {
		re := regexp.MustCompile(`(?m)` + rule.pat)
		s = re.ReplaceAllString(s, rule.repl)
	}
	return s
}

func generateJSON(path string, jv interface{}, sections map[string]string) error {
	jv, err := rewriteForJSON("", jv)
	if err != nil {
		return errgo.Notef(err, "cannot make JSON object")
	}
	buf := new(bytes.Buffer)
	enc := json.NewEncoder(buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	for _, v := range jv.([]interface{}) {
		if err := enc.Encode(v); err != nil {
			return errgo.Notef(err, "cannot marshal JSON")
		}
	}
	data, err := jqize(buf.Bytes())
	if err != nil {
		return errgo.Mask(err)
	}
	sections["in-json"] = string(data)
	var all []byte
	for _, name := range []string{
		"header",
		"in-yaml",
		"in-json",
		"error",
		"out-yaml",
		"emit-yaml",
		"test-event",
		"lex-token",
	} {
		content, ok := sections[name]
		if !ok {
			continue
		}
		if name != "header" {
			all = append(all, fmt.Sprintf("\n+++ %s\n", name)...)
		}
		all = append(all, content...)
	}
	if err := ioutil.WriteFile(path, all, 0666); err != nil {
		return errgo.Mask(err)
	}
	return nil
}

func jqize(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	c := exec.Command("jq", ".")
	c.Stdout = &buf
	c.Stdin = bytes.NewReader(data)
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return nil, errgo.Notef(err, "jq failed")
	}
	return buf.Bytes(), nil
}

func checkYAML(path string, inYAML string, expectv interface{}) error {
	var yv interface{}
	if err := yaml.Unmarshal([]byte(inYAML), &yv); err != nil {
		return errgo.Notef(err, "cannot unmarshal YAML %q", inYAML)
	}
	diff := cmp.Diff(yv, expectv)
	if diff == "" {
		return nil
	}
	return errgo.Newf("YAML differs from expected output: %v (got %v want %v)", diff, pretty.Sprint(yv), pretty.Sprint(expectv))
}

func valueFromEvents(r io.Reader) (v interface{}, rerr error) {
	defer func() {
		err := recover()
		if err == nil {
			return
		}
		if err, ok := err.(yamlError); ok {
			rerr = errgo.Newf("%s", err)
			return
		}
		panic(err)
	}()
	return doStream(newEventReader(r)), nil
}

type eventKind int

const (
	kindStream eventKind = iota + 1
	kindDoc
	kindMap
	kindSequence
	kindVal
	kindAlias
	kindError
)

var eventNames = map[string]eventKind{
	"STR": kindStream,
	"DOC": kindDoc,
	"MAP": kindMap,
	"SEQ": kindSequence,
	"VAL": kindVal,
	"ALI": kindAlias,
}

type errorEvent struct {
	err error
}

func (errorEvent) kind() eventKind { return kindError }

type streamEvent struct{}

func (streamEvent) kind() eventKind { return kindStream }

type docEvent struct {
	separator string
}

func (docEvent) kind() eventKind { return kindDoc }

type mapEvent struct {
	anchor string
	tag    string
}

func (mapEvent) kind() eventKind { return kindMap }

type sequenceEvent struct {
	anchor string
	tag    string
}

func (sequenceEvent) kind() eventKind { return kindSequence }

type valEvent struct {
	anchor string
	tag    string
	quote  rune
	val    string
}

func (valEvent) kind() eventKind { return kindVal }

type aliasEvent struct {
	anchor string
}

func (aliasEvent) kind() eventKind { return kindAlias }

type endEvent struct {
	kind_ eventKind
}

func (e endEvent) kind() eventKind {
	return -e.kind_
}

type event interface {
	kind() eventKind
}

func rewriteForJSON(path string, x interface{}) (interface{}, error) {
	switch x := x.(type) {
	case map[interface{}]interface{}:
		nm := make(map[string]interface{})
		for k, v := range x {
			var k1 string
			switch k := k.(type) {
			case string:
				k1 = k
			case int, uint, int64, uint64, float64:
				k1 = fmt.Sprint(k)
			case nil:
				return nil, errgo.Newf("cannot use null (at %s.%v) as map key", path, k)
			default:
				return nil, fmt.Errorf("map key at %s.%v (type %T) is not supported", path, k, v)
			}
			v1, err := rewriteForJSON(path+"."+k1, v)
			if err != nil {
				return nil, err
			}
			nm[k1] = v1
		}
		return nm, nil
	case []interface{}:
		nx := make([]interface{}, len(x))
		for i := range x {
			v, err := rewriteForJSON(fmt.Sprintf("path[%d]", i), x[i])
			if err != nil {
				return nil, err
			}
			nx[i] = v
		}
		return nx, nil
	case int64:
		return float64(x), nil
	case int:
		return float64(x), nil
	default:
		return x, nil
	}
}

func doStream(r *eventReader) []interface{} {
	r.expect(kindStream)
	var docs []interface{}
	for r.peek().kind() == kindDoc {
		docs = append(docs, doDoc(r))
	}
	r.expect(-kindStream)
	return docs
}

type refMap map[string]interface{}

func doDoc(r *eventReader) interface{} {
	r.expect(kindDoc)
	refs := make(refMap)
	n := doNode(r, refs)
	r.expect(-kindDoc)
	return n
}

func doNode(r *eventReader, refs refMap) interface{} {
	var val interface{}
	switch r.peek().kind() {
	case kindMap:
		val = doMap(r, refs)
	case kindVal:
		val = doVal(r, refs)
	case kindSequence:
		val = doSequence(r, refs)
	case kindAlias:
		val = doAlias(r, refs)
	default:
		failf("unexpected event; want map, value, sequence or alias, got %q (%#v)", r.scanner.Text(), r.peek())
	}
	return val
}

func doMap(r *eventReader, refs refMap) interface{} {
	m := make(map[interface{}]interface{})
	e := r.read().(mapEvent)
	for r.peek().kind() != -kindMap {
		key := doNode(r, refs)
		val := doNode(r, refs)
		func() {
			defer func() {
				if recover() != nil {
					failf("cannot use type %T as map key", key)
				}
			}()
			m[key] = val
		}()
	}
	r.expect(-kindMap)
	if e.anchor != "" {
		refs[e.anchor] = m
	}
	return m
}

func doSequence(r *eventReader, refs refMap) interface{} {
	e := r.read().(sequenceEvent)
	seq := []interface{}{}
	for r.peek().kind() != -kindSequence {
		seq = append(seq, doNode(r, refs))
	}
	r.expect(-kindSequence)
	if e.anchor != "" {
		refs[e.anchor] = seq
	}
	return seq
}

var acceptableTagsForGenerate = map[string]bool{
	"":             true,
	yaml_NULL_TAG:  true,
	yaml_BOOL_TAG:  true,
	yaml_STR_TAG:   true,
	yaml_INT_TAG:   true,
	yaml_FLOAT_TAG: true,
	yaml_SEQ_TAG:   true,
	yaml_MAP_TAG:   true,
}

func doVal(r *eventReader, refs refMap) interface{} {
	e := r.read().(valEvent)
	if *generate && !acceptableTagsForGenerate[e.tag] {
		failf("unacceptable tag %q found", e.tag)
	}
	var val interface{}
	switch e.quote {
	case ':':
		_, val = resolve(e.tag, e.val)
	default:
		val = e.val
	}
	if e.anchor != "" {
		refs[e.anchor] = val
	}
	return val
}

func doAlias(r *eventReader, refs refMap) interface{} {
	e := r.read().(aliasEvent)
	if val, ok := refs[e.anchor]; ok {
		return val
	}
	failf("reference to undefined anchor %q", e.anchor)
	panic("unreachable")
}

type eventReader struct {
	scanner *bufio.Scanner
	e       event
	err     error
}

func newEventReader(r io.Reader) *eventReader {
	return &eventReader{
		scanner: bufio.NewScanner(r),
	}
}

func (r *eventReader) peek() event {
	if r.e != nil {
		return r.e
	}
	r.e = r.read()
	return r.e
}

func (r *eventReader) expect(want eventKind) {
	e := r.read()
	if e.kind() != want {
		failf("unexpected event; want %v got %q (%#v)", want, r.scanner.Text(), r.peek())
	}
}

func (r *eventReader) read() event {
	if r.e != nil {
		e := r.e
		r.e = nil
		return e
	}
	if !r.scanner.Scan() {
		err := r.scanner.Err()
		if err == nil {
			err = io.EOF
		}
		return errorEvent{
			err: err,
		}
	}
	e, err := parseEvent(r.scanner.Text())
	if err != nil {
		e = errorEvent{err}
	}
	return e
}

func parseEvent(s string) (event, error) {
	s0 := s
	if len(s) == 0 {
		return nil, errgo.Newf("empty event")
	}
	var start bool
	switch s[0] {
	case '-':
		start = false
	case '+', '=':
		start = true
	default:
		return nil, errgo.Newf("unknown event start character in %q", s0)
	}
	s = s[1:]
	kindStr, s := nextField(s)
	kind, ok := eventNames[kindStr]
	if !ok {
		return nil, errgo.Newf("unknown event name in %q", s0)
	}
	if !start {
		return endEvent{
			kind_: kind,
		}, nil
	}
	switch kind {
	case kindStream:
		return streamEvent{}, nil
	case kindDoc:
		return docEvent{
			separator: s,
		}, nil
	case kindMap:
		e := mapEvent{}
		if strings.HasPrefix(s, "&") {
			e.anchor, s = nextField(s)
			e.anchor = e.anchor[1:]
		}
		if strings.HasPrefix(s, "<") {
			e.tag, s = nextField(s)
			e.tag = strings.TrimSuffix(e.tag[1:], ">")
		}
		if s != "" {
			return nil, errgo.Newf("extra fields at end of map event %q", s0)
		}
		return e, nil
	case kindSequence:
		e := sequenceEvent{}
		if strings.HasPrefix(s, "&") {
			e.anchor, s = nextField(s)
			e.anchor = e.anchor[1:]
		}
		if strings.HasPrefix(s, "<") {
			e.tag, s = nextField(s)
			e.tag = strings.TrimSuffix(e.tag[1:], ">")
		}
		if s != "" {
			return nil, errgo.Newf("extra fields at end of sequence event %q", s0)
		}
		return e, nil
	case kindVal:
		e := valEvent{}
		if strings.HasPrefix(s, "&") {
			e.anchor, s = nextField(s)
			e.anchor = e.anchor[1:]
		}
		if strings.HasPrefix(s, "<") {
			e.tag, s = nextField(s)
			e.tag = strings.TrimSuffix(e.tag[1:], ">")
		}
		if s == "" {
			return nil, errgo.Newf("no value in value event %q", s0)
		}
		switch s[0] {
		case ':', '\'', '"', '>', '|':
			e.quote = rune(s[0])
		default:
			return nil, errgo.Newf("unexpected quote kind in value event %q", s0)
		}
		e.val = unquoter.Replace(s[1:])
		return e, nil
	case kindAlias:
		if !strings.HasPrefix(s, "*") {
			return nil, errgo.Newf("unexpected alias value in %q", s0)
		}
		return aliasEvent{
			anchor: s[1:],
		}, nil
	default:
		panic("unreachable")
	}
}

var unquoter = strings.NewReplacer("\\n", "\n", "\\t", "\t", "\\\\", "\\", "\\b", "\b", "\\r", "\r")

func nextField(s string) (string, string) {
	i := strings.Index(s, " ")
	if i < 0 {
		return s, ""
	}
	return s[:i], s[i+1:]
}

type yamlError string

func failf(f string, a ...interface{}) {
	panic(yamlError(fmt.Sprintf(f, a...)))
}
