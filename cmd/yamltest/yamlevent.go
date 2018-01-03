package main

import (
	"bufio"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/go-cmp/cmp"
	"github.com/kr/pretty"
	errgo "gopkg.in/errgo.v1"
	yaml "gopkg.in/yaml.v1"
)

func main() {
	for _, f := range os.Args[1:] {
		if err := check(f); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %s\n", f, err)
		}
	}
}

func check(path string) error {
	eventf, err := os.Open(filepath.Join(path, "test.event"))
	if err != nil {
		return err
	}
	defer eventf.Close()
	inYAML, err := ioutil.ReadFile(filepath.Join(path, "in.yaml"))
	if err != nil {
		return err
	}
	v, err := valueFromEvents(eventf)
	if err != nil {
		return errgo.Notef(err, "cannot make object")
	}
	if vs, ok := v.([]interface{}); ok && len(vs) > 0 {
		// The in.json values only include a single document.
		if len(vs) > 1 {
			return errgo.Newf("cannot represent multiple documents as JSON")
		}
		v = vs[0]
	} else {
		v = nil
	}
	jv, err := rewriteForJSON("", v)
	if err != nil {
		return errgo.Notef(err, "cannot make JSON object")
	}
	var inYAMLv interface{}
	if err := yaml.Unmarshal(inYAML, &inYAMLv); err != nil {
		return errgo.Notef(err, "cannot unmarshal YAML")
	}
	yv, err := rewriteForJSON("", inYAMLv)
	if err != nil {
		return errgo.Notef(err, "cannot make JSON object from YAML")
	}
	if diff := cmp.Diff(yv, jv); diff != "" {
		return errgo.Newf("YAML differs from expected output: %v (got %v want %v)", diff, pretty.Sprint(yv), pretty.Sprint(jv))
	}
	return nil
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
				k1 = ""
			default:
				return nil, fmt.Errorf("map key at %s.%v (type %T) is not supported", path, x, k)
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
	return resolveAnchors(n, refs)
}

func resolveAnchors(n interface{}, refs refMap) interface{} {
	switch n := n.(type) {
	case map[interface{}]interface{}:
		m := make(map[interface{}]interface{})
		for k, v := range n {
			m[resolveAnchors(k, refs)] = resolveAnchors(v, refs)
		}
		return m
	case []interface{}:
		s := make([]interface{}, len(n))
		for i, v := range n {
			s[i] = resolveAnchors(v, refs)
		}
		return s
	case lazyAlias:
		v, ok := refs[string(n)]
		if !ok {
			failf("anchor %q not found", n)
		}
		return v
	default:
		return n
	}
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
	var seq []interface{}
	for r.peek().kind() != -kindSequence {
		seq = append(seq, doNode(r, refs))
	}
	r.expect(-kindSequence)
	if e.anchor != "" {
		refs[e.anchor] = seq
	}
	return seq
}

func doVal(r *eventReader, refs refMap) interface{} {
	e := r.read().(valEvent)
	var val interface{}
	switch e.quote {
	case ':':
		_, val = resolve("", e.val)
	default:
		val = e.val
	}
	if e.anchor != "" {
		refs[e.anchor] = val
	}
	return val
}

type lazyAlias string

func doAlias(r *eventReader, refs refMap) interface{} {
	e := r.read().(aliasEvent)
	if val, ok := refs[e.anchor]; ok {
		return val
	}
	return lazyAlias(e.anchor)
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
			e.tag = e.tag[1:]
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
			e.tag = e.tag[1:]
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
			e.tag = e.tag[1:]
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
