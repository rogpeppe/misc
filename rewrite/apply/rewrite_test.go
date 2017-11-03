// Copyright 2017 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package apply_test

import (
	"bytes"
	"github.com/rogpeppe/misc/rewrite/apply"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"testing"
)

var rewriteTests = [...]struct {
	name      string
	in, out   string
	pre, post apply.ApplyFunc
}{

	{name: "nop", in: "package p\n", out: "package p\n", pre: nil, post: nil},

	{name: "insert",
		in: `package p

var (
	x int
	y int
)
`,
		out: `package p

var before1 int
var before2 int

var (
	x int
	y int
)
var after2 int
var after1 int
`,
		pre: func(cursor *apply.ApplyCursor) bool {
			n := cursor.Node()
			_, ok := n.(*ast.GenDecl)
			if !ok {
				return true
			}

			cursor.InsertBefore(vardecl("before1", "int"))
			cursor.InsertAfter(vardecl("after1", "int"))
			cursor.InsertAfter(vardecl("after2", "int"))
			cursor.InsertBefore(vardecl("before2", "int"))
			return true
		},
	},

	{name: "delete",
		in: `package p

var x int
var y int
var z int
`,
		out: `package p

var y int
var z int
`,
		pre: func(cursor *apply.ApplyCursor) bool {
			n := cursor.Node()
			if d, ok := n.(*ast.GenDecl); ok && d.Specs[0].(*ast.ValueSpec).Names[0].Name == "x" {
				cursor.Delete()
			}
			return true
		},
	},

	{name: "insertafter-delete",
		in: `package p

var x int
var y int
var z int
`,
		out: `package p

var x1 int

var y int
var z int
`,
		pre: func(cursor *apply.ApplyCursor) bool {
			n := cursor.Node()
			if d, ok := n.(*ast.GenDecl); ok && d.Specs[0].(*ast.ValueSpec).Names[0].Name == "x" {
				cursor.InsertAfter(vardecl("x1", "int"))
				cursor.Delete()
			}
			return true
		},
	},

	{name: "delete-insertafter",
		in: `package p

var x int
var y int
var z int
`,
		out: `package p

var y int
var x1 int
var z int
`,
		pre: func(cursor *apply.ApplyCursor) bool {
			n := cursor.Node()
			if d, ok := n.(*ast.GenDecl); ok && d.Specs[0].(*ast.ValueSpec).Names[0].Name == "x" {
				cursor.Delete()
				// The cursor is now effectively atop the 'var y int' node.
				cursor.InsertAfter(vardecl("x1", "int"))
			}
			return true
		},
	},
}

func vardecl(name string, typ string) ast.Node {
	return &ast.GenDecl{
		Tok: token.VAR,
		Specs: []ast.Spec{
			&ast.ValueSpec{
				Names: []*ast.Ident{ast.NewIdent(name)},
				Type:  ast.NewIdent(typ),
			},
		},
	}
}

func TestRewrite(t *testing.T) {
	t.Run("*", func(t *testing.T) {
		for _, test := range rewriteTests {
			test := test
			t.Run(test.name, func(t *testing.T) {
				t.Parallel()
				fset := token.NewFileSet()
				f, err := parser.ParseFile(fset, "rewrite.go", test.in, parser.ParseComments)
				if err != nil {
					t.Fatal(err)
				}
				n := apply.Apply(f, test.pre, test.post)
				var buf bytes.Buffer
				if err := format.Node(&buf, fset, n); err != nil {
					t.Fatal(err)
				}
				got := buf.String()
				if got != test.out {
					t.Errorf("want:\n\n%s\ngot:\n\n%s\n", test.out, got)
				}
			})
		}
	})
}

var sink ast.Node

func BenchmarkRewrite(b *testing.B) {
	for _, test := range rewriteTests {
		b.Run(test.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				fset := token.NewFileSet()
				f, err := parser.ParseFile(fset, "rewrite.go", test.in, parser.ParseComments)
				if err != nil {
					b.Fatal(err)
				}
				b.StartTimer()
				sink = apply.Apply(f, test.pre, test.post)
			}
		})
	}
}
