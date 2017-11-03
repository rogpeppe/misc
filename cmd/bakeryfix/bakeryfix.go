package main

import (
	"flag"
	"go/ast"
	"go/build"
	"log"
	"os"

	"github.com/kisielk/gotool"
	"github.com/rogpeppe/misc/rewrite"
	"github.com/rogpeppe/misc/rewrite/apply"
)

func main() {
	flag.Parse()
	pkgs := gotool.ImportPaths(flag.Args())
	srcDir, _ := os.Getwd()

	changed, err := rewrite.Rewrite(&build.Default, srcDir, pkgs, getApplyFuncs)
	if err != nil {
		log.Printf("rewrite error: %v", err)
	}
	log.Printf("changed %q", changed)
}

type applier struct {
	pkg *rewrite.Package
}

const bakeryPath = "gopkg.in/macaroon-bakery.v2-unstable/bakery"

func getApplyFuncs(pkg *rewrite.Package) (pre, post apply.ApplyFunc) {
	if pkg.Prog.Package(bakeryPath) == nil {
		// Can't be any instances of the types we're looking for if
		// bakery isn't there.
		return nil, nil
	}
	a := &applier{
		pkg: pkg,
	}
	return a.pre, nil
}

var movedToIdentChecker = map[string]bool{
	"ACLIdentity":                 true,
	"Bakery":                      true,
	"BakeryParams":                true,
	"Checker":                     true,
	"CheckerParams":               true,
	"Authorizer":                  true,
	"Everyone":                    true,
	"Identity":                    true,
	"IdentityClient":              true,
	"LoginOp":                     true,
	"NewChecker":                  true,
	"OpenAuthorizerCheckerParams": true,
	"SimpleIdentity":              true,
}

const identCheckerPath = bakeryPath + "/identchecker"

func (a *applier) pre(c *apply.ApplyCursor) bool {
	switch n := c.Node().(type) {
	case *ast.SelectorExpr:
		if !a.pkg.IsPkgName(n.X, bakeryPath) {
			break
		}
		if movedToIdentChecker[n.Sel.Name] {
			c.Replace(&ast.SelectorExpr{
				X:   a.pkg.PackageIdent(identCheckerPath, ""),
				Sel: n.Sel,
			})
			break
		}
		switch n.Sel.Name {
		case "New":
			c.Replace(&ast.SelectorExpr{
				X:   a.pkg.PackageIdent(identCheckerPath, ""),
				Sel: &ast.Ident{Name: "NewBakery"},
			})
		case "MacaroonOpStore":
			c.Replace(&ast.SelectorExpr{
				X:   a.pkg.PackageIdent(identCheckerPath, ""),
				Sel: &ast.Ident{Name: "MacaroonVerifier"},
			})
		}
	}
	return true
}
