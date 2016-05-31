// The jujuapidoc command generates a JSON file containing
// details of as many Juju RPC calls as it can get its hands on.
//
// It depends on a custom addition to the apiserver package,
// FacadeRegistry.ListDetails, the implementation of which
// can be found in https://github.com/rogpeppe/juju/tree/076-apiserver-facade-list-details.
//
// The resulting JSON output can be processed into HTML by
// the jujuapidochtml command.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	_ "github.com/juju/juju/apiserver"
	"github.com/juju/juju/apiserver/common"
	"github.com/juju/juju/rpc/rpcreflect"
	"github.com/rogpeppe/apicompat/jsontypes"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"golang.org/x/tools/go/loader"
	"gopkg.in/errgo.v1"
	"html/template"
	"log"
	"os"
	"reflect"

	"github.com/rogpeppe/misc/cmd/jujuapidoc/apidoc"
)

func main() {
	flag.Parse()

	info, err := generateInfo()
	if err != nil {
		log.Fatal(err)
	}
	data, err := json.Marshal(info)
	if err != nil {
		log.Fatal(err)
	}
	os.Stdout.Write(data)
}

func generateInfo() (*apidoc.Info, error) {
	serverPkg := "github.com/juju/juju/apiserver"
	cfg := loader.Config{
		TypeCheckFuncBodies: func(string) bool {
			return true
		},
		ImportPkgs: map[string]bool{
			serverPkg: false, // false means don't load tests.
		},
		ParserMode: parser.ParseComments,
	}
	prog, err := cfg.Load()
	if err != nil {
		return nil, errgo.Notef(err, "cannot load %q", serverPkg)
	}

	info := jsontypes.NewInfo()
	for _, d := range common.Facades.ListDetails() {
		t := rpcreflect.ObjTypeOf(d.Type)

		for _, name := range t.MethodNames() {
			m, _ := t.Method(name)
			if m.Params != nil {
				info.TypeInfo(m.Params)
			}
			if m.Result != nil {
				info.TypeInfo(m.Result)
			}
		}
	}
	apiInfo := &apidoc.Info{
		TypeInfo: info,
	}
	for _, d := range common.Facades.ListDetails() {
		f := apidoc.FacadeInfo{
			Name:    d.Name,
			Version: d.Version,
		}
		pt, err := progType(prog, d.Type)
		if err != nil {
			return nil, errgo.Notef(err, "cannot get prog type for %v", d.Type)
		}
		tdoc, err := typeDocComment(prog, pt)
		if err != nil {
			return nil, errgo.Notef(err, "cannot get doc comment for %v: %v", d.Type)
		}
		f.Doc = tdoc
		t := rpcreflect.ObjTypeOf(d.Type)
		for _, name := range t.MethodNames() {
			m, _ := t.Method(name)
			fm := apidoc.Method{
				Name: name,
			}
			if m.Params != nil {
				fm.Param = info.Ref(m.Params)
			}
			if m.Result != nil {
				fm.Result = info.Ref(m.Result)
			}
			mdoc, err := methodDocComment(prog, pt, name)
			if err != nil {
				return nil, errgo.Notef(err, "cannot get doc comment for %v.%v: %v", d.Type, name)
			}
			fm.Doc = mdoc
			f.Methods = append(f.Methods, fm)
		}
		apiInfo.Facades = append(apiInfo.Facades, f)
	}
	return apiInfo, nil
}

var tmplFuncs = template.FuncMap{
	"typeLink": func(t *jsontypes.Type) template.HTML {
		if t == nil {
			return "n/a"
		}
		link := fmt.Sprintf(`<a href="https://godoc.org/%s">%s</a>`, t.Name, t.Name.Name())
		return template.HTML(link)
	},
}

func methodDocComment(prog *loader.Program, tname *types.TypeName, methodName string) (string, error) {
	t := tname.Type()
	if !types.IsInterface(t) {
		// Use the pointer type to get as many methods as possible.
		t = types.NewPointer(t)
	}

	mset := types.NewMethodSet(t)
	sel := mset.Lookup(nil, methodName)
	if sel == nil {
		return "", errgo.Newf("cannot find method %v on %v", methodName, t)
	}
	obj := sel.Obj()
	decl, err := findDecl(prog, obj.Pos())
	if err != nil {
		return "", errgo.Mask(err)
	}
	switch decl := decl.(type) {
	case *ast.GenDecl:
		if decl.Tok != token.TYPE {
			return "", errgo.Newf("found non-type decl %#v", decl)
		}
		for _, spec := range decl.Specs {
			tspec := spec.(*ast.TypeSpec)
			it := tspec.Type.(*ast.InterfaceType)
			for _, m := range it.Methods.List {
				for _, id := range m.Names {
					if id.Pos() == obj.Pos() {
						return m.Doc.Text(), nil
					}
				}
			}
		}
		return "", errgo.Newf("method definition not found in type")
	case *ast.FuncDecl:
		if decl.Name.Pos() != obj.Pos() {
			return "", errgo.Newf("method definition not found (at %#v)", prog.Fset.Position(obj.Pos()))
		}
		return decl.Doc.Text(), nil
	default:
		return "", errgo.Newf("unexpected declaration %T found", decl)
	}
}

func typeDocComment(prog *loader.Program, t *types.TypeName) (string, error) {
	decl, err := findDecl(prog, t.Pos())
	if err != nil {
		return "", errgo.Mask(err)
	}
	tdecl, ok := decl.(*ast.GenDecl)
	if !ok || tdecl.Tok != token.TYPE {
		return "", errgo.Newf("found non-type decl %#v", decl)
	}
	for _, spec := range tdecl.Specs {
		tspec := spec.(*ast.TypeSpec)
		if tspec.Name.Pos() == t.Pos() {
			if tspec.Doc != nil {
				return tspec.Doc.Text(), nil
			}
			return tdecl.Doc.Text(), nil
		}
	}
	return "", errgo.Newf("cannot find type declaration")
}

// findDecl returns the top level declaration that contains the
// given position.
func findDecl(prog *loader.Program, pos token.Pos) (ast.Decl, error) {
	tokFile := prog.Fset.File(pos)
	if tokFile == nil {
		return nil, errgo.Newf("no file found for object")
	}
	filename := tokFile.Name()
	for _, pkgInfo := range prog.AllPackages {
		for _, f := range pkgInfo.Files {
			if tokFile := prog.Fset.File(f.Pos()); tokFile == nil || tokFile.Name() != filename {
				continue
			}
			// We've found the file we're looking for. Now traverse all
			// top level declarations looking for the right function declaration.
			for _, decl := range f.Decls {
				if decl.Pos() <= pos && pos <= decl.End() {
					return decl, nil
				}
			}
		}
	}
	return nil, errgo.Newf("declaration not found")
}

// progType returns the go/types type for the given reflect.Type,
// which must represent a named non-predeclared Go type.
func progType(prog *loader.Program, t reflect.Type) (*types.TypeName, error) {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	typeName := t.Name()
	if typeName == "" {
		return nil, errgo.Newf("type %s is not named", t)
	}
	pkgPath := t.PkgPath()
	if pkgPath == "" {
		// TODO could return types.Basic type here if we needed to.
		return nil, errgo.Newf("type %s not declared in package", t)
	}
	pkgInfo := prog.Package(pkgPath)
	if pkgInfo == nil {
		return nil, errgo.Newf("cannot find %q in imported code", pkgPath)
	}
	pkg := pkgInfo.Pkg
	obj := pkg.Scope().Lookup(typeName)
	if obj == nil {
		return nil, errgo.Newf("type %s not found in %s", typeName, pkgPath)
	}
	objTypeName, ok := obj.(*types.TypeName)
	if !ok {
		return nil, errgo.Newf("%s is not a type", typeName)
	}
	return objTypeName, nil
}

// in github.com/juju/juju/apiserver/common
//
//package common
//
//type FacadeDetails struct {
//	Name string
//	Version int
//	Factory interface{}
//	Type reflect.Type
//	Feature string
//}
//
//func (f *FacadeRegistry) ListDetails() []FacadeDetails {
//	names := make([]string, 0, len(f.facades))
//	for name := range f.facades {
//		names = append(names, name)
//	}
//	sort.Strings(names)
//	var details []FacadeDetails
//	for _, name := range names {
//		for v, info := range f.facades[name] {
//			details = append(details, FacadeDetails{
//				Name: name,
//				Version: v,
//				Factory: info.factory,
//				Type: info.facadeType,
//				Feature: info.feature,
//			})
//		}
//	}
//	return details
//}
