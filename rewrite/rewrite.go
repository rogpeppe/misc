package rewrite

import (
	"bytes"
	"go/ast"
	"go/build"
	"go/format"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/rogpeppe/misc/rewrite/apply"

	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/go/loader"
	errgo "gopkg.in/errgo.v1"
)

type processedInfo struct {
	processed    map[string]bool
	changedFiles []string
}

type Package struct {
	Prog *loader.Program
	*loader.PackageInfo
	imports    map[string]map[string]bool
	currentPos token.Pos
}

// isPkgDot reports whether t is a selector
// expression of the form x.name where x refers to the
// package with the given import path.
func (pkg *Package) IsPkgDot(t ast.Node, importPath, name string) bool {
	sel, ok := t.(*ast.SelectorExpr)
	return ok && pkg.IsPkgName(sel.X, importPath) && sel.Sel.String() == name
}

// IsPkgName reports whether t refers to a package with the
// given import path.
func (pkg *Package) IsPkgName(t ast.Node, importPath string) bool {
	id, ok := t.(*ast.Ident)
	if !ok || id == nil {
		return false
	}
	pkgName, ok := pkg.Uses[id].(*types.PkgName)
	if !ok {
		return false
	}
	return pkgName.Imported().Path() == importPath
}

func (pkg *Package) posSetter(f apply.ApplyFunc) apply.ApplyFunc {
	if f == nil {
		return nil
	}
	return func(c *apply.ApplyCursor) bool {
		pkg.currentPos = c.Node().Pos()
		return f(c)
	}
}

// PackageName returns a new identifier that refers to the given import
// path. Rewrite will ensure that the named package will be imported by
// the file being rewritten. The identifier argument holds the preferred
// identifier for the package, which will be used if the package doesn't
// already a local identifier. If identifier is empty, a suitable name
// will be chosen automatically.
func (pkg *Package) PackageIdent(importPath, identifier string) *ast.Ident {
	if ids := pkg.imports[importPath]; len(ids) > 0 {
		first := ""
		// Choose the identifier that's first alphabetically
		// so that we're deterministic.
		for id := range ids {
			if first == "" || id < first {
				first = id
			}
		}
		identifier = first
	} else {
		pkg.imports[importPath] = make(map[string]bool)
	}
	lpkg := pkg.Prog.Package(importPath)
	if identifier == "" {
		if lpkg != nil {
			identifier = lpkg.Pkg.Name()
		} else {
			// TODO Could try to do a go/build import of the package name too.
			// TODO do better for packages with trailing version numbers.
			if i := strings.LastIndex(importPath, "/"); i >= 0 {
				identifier = importPath[i+1:]
			} else {
				identifier = importPath
			}
		}
		// TODO choose a different identifier if this one's already used.
	}
	var tpkg *types.Package
	if lpkg != nil {
		tpkg = lpkg.Pkg
	} else {
		tpkg = types.NewPackage(importPath, identifier)
	}
	ident := &ast.Ident{
		Name:    identifier,
		NamePos: pkg.currentPos,
	}
	// Record the fact that it's used so that we retain it in the
	// final import list.
	pkg.Uses[ident] = types.NewPkgName(token.NoPos, pkg.Pkg, identifier, tpkg)
	// Note that pkg.imports may be nil if PackageIdent is called
	// before we've started processing a file, so be defensive
	// in that case.
	if pkg.imports != nil {
		pkg.imports[importPath][identifier] = true
	}
	return ident
}

// Rewrite reads, parses and type checks the Go code in all the given
// packages, and traverses the syntax tree of each file,
// calling pre and post for each node as described in the apply
// package. Any changes made to the syntax tree will be
// written to the respective file.
//
// Rewrite returns a slice holding the names of all the packages
// that have been changed.
func Rewrite(
	buildCtx *build.Context,
	srcDir string,
	paths []string,
	getApplyFuncs func(pkg *Package) (pre, post apply.ApplyFunc),
) ([]string, error) {
	pkgs := make(map[string]*build.Package)
	allFiles := make(map[string]bool)
	for _, path := range paths {
		pkg, err := buildCtx.Import(path, srcDir, 0)
		if err != nil {
			log.Printf("warning: import %q failed: %v", path, err)
			continue
		}
		pkgs[pkg.ImportPath] = pkg
		for _, goFiles := range [][]string{
			pkg.GoFiles,
			pkg.CgoFiles,
			pkg.IgnoredGoFiles,
			pkg.InvalidGoFiles,
		} {
			for _, path := range goFiles {
				if !filepath.IsAbs(path) {
					path = filepath.Join(pkg.Dir, path)
				}
				allFiles[path] = true
			}
		}
	}
	newConfig := func() *loader.Config {
		return &loader.Config{
			Fset:       token.NewFileSet(),
			ParserMode: parser.ParseComments,
			TypeChecker: types.Config{
				Error: func(err error) {
					//log.Printf("type check error: %v", err)
				},
				DisableUnusedImportCheck: true,
			},
			TypeCheckFuncBodies: func(path string) bool {
				return pkgs[path] != nil
			},
			Build:       buildCtx,
			Cwd:         srcDir,
			AllowErrors: true,
		}
	}
	cfg := newConfig()
	for path, pkg := range pkgs {
		if len(pkg.TestGoFiles) == 0 {
			cfg.ImportWithTests(path)
		} else {
			// There are internal test files that may modify the types,
			// so process the non-test code first, and process
			// the rest of the files later.
			cfg.Import(path)
		}
	}
	pinfo := &processedInfo{
		processed: make(map[string]bool),
	}
	if err := process(cfg, pinfo, getApplyFuncs); err != nil {
		return nil, errgo.Mask(err)
	}
	// Now, for each package with an internal test file, load it separately
	// and run the same process on any new files.
	for path, pkg := range pkgs {
		if len(pkg.TestGoFiles) == 0 {
			// Already processed in full.
			continue
		}
		cfg := newConfig()
		cfg.ImportWithTests(path)
		// TODO parallelize this?
		if err := process(cfg, pinfo, getApplyFuncs); err != nil {
			return nil, errgo.Mask(err)
		}
	}
	var unprocessed []string
	for path := range allFiles {
		if !pinfo.processed[path] {
			unprocessed = append(unprocessed, path)
		}
	}
	if len(unprocessed) == 0 {
		log.Printf("no files unprocessed")
	} else {
		log.Printf("unprocessed files: %q", unprocessed)
	}
	sort.Strings(pinfo.changedFiles)
	return pinfo.changedFiles, nil
}

func process(cfg *loader.Config, pinfo *processedInfo, getApplyFuncs func(pkg *Package) (pre, post apply.ApplyFunc)) error {
	prog, err := cfg.Load()
	if err != nil {
		if prog == nil {
			return errgo.Notef(err, "load failed")
		}
		log.Printf("warning: load failed: %v", err)
	}
	for name, pkgInfo := range prog.Imported {
		pkg := &Package{
			Prog:        prog,
			PackageInfo: pkgInfo,
		}
		pre, post := getApplyFuncs(pkg)
		// Make sure the position is always set correctly
		// so that PackageIdent can return an identifier
		// with roughly the right position.
		pre = pkg.posSetter(pre)
		post = pkg.posSetter(post)

		for _, file := range pkgInfo.Files {
			pos := prog.Fset.Position(file.Pos())
			if !pos.IsValid() {
				log.Printf("no filename found for file in package %q", name)
				if pinfo.processed[pos.Filename] {
					continue
				}
				continue
			}
			pinfo.processed[pos.Filename] = true
			if pre == nil && post == nil {
				// No point in calling apply if there are no processing functions.
				continue
			}
			pkg.imports = fileImports(pkg, file)

			newFile := apply.Apply(file, pre, post).(*ast.File)
			newFile = updateImports(pkg, newFile)

			changed, err := updateFile(pos.Filename, newFile, prog.Fset)
			if err != nil {
				log.Printf("cannot update %q: %v", pos.Filename, err)
			} else if changed {
				pinfo.changedFiles = append(pinfo.changedFiles, pos.Filename)
			}
		}
	}
	return nil
}

func litToString(lit *ast.BasicLit) string {
	if lit.Kind != token.STRING {
		panic("unexpected kind for BasicLit")
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		panic(err)
	}
	return s
}

func fileImports(pkg *Package, file *ast.File) map[string]map[string]bool {
	imports := make(map[string]map[string]bool)
	for _, specs := range astutil.Imports(pkg.Prog.Fset, file) {
		for _, spec := range specs {
			path := litToString(spec.Path)
			if imports[path] == nil {
				imports[path] = make(map[string]bool)
			}
			if spec.Name != nil {
				// TODO what should we do when Name is "."?
				imports[path][spec.Name.Name] = true
				continue
			}
			impPkg := pkg.Prog.Package(path)
			if impPkg == nil {
				log.Printf("cannot find type package for %q", path)
				continue
			}
			imports[path][impPkg.Pkg.Name()] = true
		}
	}
	return imports
}

func updateImports(pkg *Package, file *ast.File) *ast.File {
	usedPkgs := make(map[string]map[string]bool)
	ast.Inspect(file, func(n ast.Node) bool {
		ident, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		pkgName, ok := pkg.Uses[ident].(*types.PkgName)
		if !ok {
			return true
		}
		if strings.Contains(pkgName.Name(), "/") {
			panic(errgo.Newf("name is %q; pkgName: %#v; pos %v", pkgName.Name(), pkgName, pkgName.Pos()))
		}
		importPath := pkgName.Imported().Path()
		if usedPkgs[importPath] == nil {
			usedPkgs[importPath] = make(map[string]bool)
		}
		usedPkgs[importPath][pkgName.Name()] = true
		return true
	})
	// TODO add _ imports to usedPkgs.

	// Add all the import paths we've used.
	for importPath, ids := range usedPkgs {
		// TODO better logic for avoiding renaming
		for id := range ids {
			if path.Base(importPath) == id {
				id = ""
			}
			astutil.AddNamedImport(pkg.Prog.Fset, file, id, importPath)
		}
	}
	// Remove the ones we don't use.
	for _, specs := range astutil.Imports(pkg.Prog.Fset, file) {
		for _, spec := range specs {
			importPath := litToString(spec.Path)
			if len(usedPkgs[importPath]) != 0 {
				continue
			}
			var name string
			if spec.Name != nil {
				name = spec.Name.Name
			}
			astutil.DeleteNamedImport(pkg.Prog.Fset, file, name, importPath)
		}
	}
	ast.SortImports(pkg.Prog.Fset, file)
	return file
}

func updateFile(path string, fileNode *ast.File, fset *token.FileSet) (bool, error) {
	var buf bytes.Buffer
	err := format.Node(&buf, fset, fileNode)
	if err != nil {
		return false, errgo.Notef(err, "cannot format data for %q", path)
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0666)
	if err != nil {
		return false, errgo.Mask(err)
	}
	defer f.Close()

	oldData, err := ioutil.ReadAll(f)
	if err != nil {
		return false, errgo.Mask(err)
	}
	if bytes.Equal(buf.Bytes(), oldData) {
		return false, nil
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return false, errgo.Notef(err, "cannot seek in %q", path)
	}
	if err := f.Truncate(0); err != nil {
		return false, errgo.Notef(err, "cannot truncate %q", path)
	}
	if _, err := f.Write(buf.Bytes()); err != nil {
		return false, errgo.Notef(err, "cannot write %q", path)
	}
	return true, nil
}
