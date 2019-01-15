package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"strings"
	"unicode"

	errgo "gopkg.in/errgo.v1"
)

type openAPIComponents struct {
	Schemas         map[string]interface{} `yaml:"schemas,omitempty"`
	SecuritySchemes map[string]interface{} `yaml:"securitySchemes,omitempty"`
}

type openAPISpec struct {
	Version    string                            `yaml:"openapi,omitempty"`
	Info       interface{}                       `yaml:"info,omitempty"`
	Paths      map[string]map[string]interface{} `yaml:"paths,omitempty"`
	Components openAPIComponents                 `yaml:"components"`
}

func (spec *openAPISpec) parse(data []byte) error {
	r := &reader{
		buf: data,
		r:   bytes.NewReader(data),
	}
	for {
		tok, err := r.readToken()
		if err != nil {
			if errgo.Cause(err) == io.EOF {
				return nil
			}
			return errgo.Mask(err)
		}
		if tok != tokenIdent {
			return errgo.Newf("unexpected token type %v", tok)
		}
		k, ok := kinds[r.token]
		if !ok {
			return errgo.Newf("unknown token %q", r.token)
		}
		var args []string
		var obj interface{}
		for {
			tok, err := r.readToken()
			if err != nil {
				if errgo.Cause(err) == io.EOF {
					return nil
				}
				return errgo.Mask(err)
			}
			if tok == tokenObject {
				obj = r.obj
				break
			}
			args = append(args, r.token)
		}
		if err := spec.add(k, args, obj); err != nil {
			return errgo.Mask(err)
		}
	}
}

func (spec *openAPISpec) add(k kind, args []string, obj interface{}) error {
	if len(args) != argCount[k] {
		return errgo.Newf("unexpected arg count for %v; got %d want %d", k, len(args), argCount[k])
	}
	switch k {
	case kindSchema:
		name := args[0]
		if spec.Components.Schemas[name] != nil {
			return errgo.Newf("schema %s redefined", name)
		}
		if spec.Components.Schemas == nil {
			spec.Components.Schemas = make(map[string]interface{})
		}
		spec.Components.Schemas[name] = obj
	case kindSecurity:
		name := args[0]
		if spec.Components.SecuritySchemes[name] != nil {
			return errgo.Newf("security scheme %s redefined", name)
		}
		if spec.Components.SecuritySchemes == nil {
			spec.Components.SecuritySchemes = make(map[string]interface{})
		}
		spec.Components.SecuritySchemes[name] = obj
	case kindPath:
		path, method := args[0], args[1]
		if !allowedMethods[method] {
			return errgo.Newf("unknown method %q for path %q", args[1], args[0])
		}
		if spec.Paths == nil {
			spec.Paths = make(map[string]map[string]interface{})
		}
		if spec.Paths[path][method] != nil {
			return errgo.Newf("redefinition of %s method for path %q", method, path)
		}
		if spec.Paths[path] == nil {
			spec.Paths[path] = make(map[string]interface{})
		}
		spec.Paths[path][method] = obj
	case kindInfo:
		if spec.Info != nil {
			return errgo.Newf("info redefined")
		}
		spec.Info = obj
	default:
		return errgo.Newf("unknown kind %v", k)
	}
	return nil
}

var allowedMethods = map[string]bool{
	"get":    true,
	"post":   true,
	"delete": true,
	"head":   true,
}

type kind int

const (
	_ kind = iota
	kindSchema
	kindSecurity
	kindPath
	kindInfo
)

var kinds = map[string]kind{
	"info":     kindInfo,
	"schema":   kindSchema,
	"security": kindSecurity,
	"path":     kindPath,
}

var argCount = map[kind]int{
	kindSchema:   1,
	kindSecurity: 1,
	kindPath:     2,
	kindInfo:     0,
}

type token int

const (
	_ token = iota
	tokenIdent
	tokenObject
)

type reader struct {
	r       *bytes.Reader
	buf     []byte
	token   string
	builder strings.Builder
	obj     interface{}
}

func (r *reader) readTokenXXX() (token, error) {
	t, err := r.readToken()
	if err != nil {
		log.Printf("readToken -> error %v", err)
		return t, err
	}
	switch t {
	case tokenObject:
		log.Printf("readToken obj %q", r.obj)
	case tokenIdent:
		log.Printf("readToken ident %q", r.token)
	}
	return t, nil
}

func (r *reader) readToken() (token, error) {
	if err := r.readSpace(); err != nil {
		return 0, err
	}
	r.builder.Reset()
	for {
		c, _, err := r.r.ReadRune()
		if err != nil {
			return 0, err
		}
		if c == '{' { // }
			r.r.UnreadRune()
			return r.readObject()
		}
		if unicode.IsSpace(c) {
			r.r.UnreadRune()
			r.token = r.builder.String()
			return tokenIdent, nil
		}
		r.builder.WriteRune(c)
	}
}

func (r *reader) readObject() (token, error) {
	dec := json.NewDecoder(r.r)
	var m interface{}
	if err := dec.Decode(&m); err != nil {
		return 0, err
	}
	offset := len(r.buf) - r.r.Len() - dec.Buffered().(*bytes.Reader).Len()
	r.r = bytes.NewReader(r.buf[offset:])
	r.obj = m
	return tokenObject, nil
}

func (r *reader) readSpace() error {
	for {
		b, _, err := r.r.ReadRune()
		if err != nil {
			return err
		}
		if !unicode.IsSpace(b) {
			r.r.UnreadRune()
			return nil
		}
	}
}
