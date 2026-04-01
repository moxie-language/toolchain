// moxiejs compiles Moxie source to JavaScript ES modules.
//
// Usage:
//
//	moxiejs [-o outputdir] [-runtime runtimedir] [-dump-ssa] <package>
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"runtime"

	"moxie/jsbackend"
	"golang.org/x/tools/go/ssa"
)

func main() {
	outputDir := flag.String("o", "jsout", "output directory for .mjs files")
	runtimeDir := flag.String("runtime", "", "path to jsruntime/ directory (if empty, must be copied manually)")
	dumpSSA := flag.Bool("dump-ssa", false, "dump SSA for debugging")
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: moxiejs [-o dir] <file.mx or package path>")
		os.Exit(1)
	}

	input := args[0]

	if filepath.Ext(input) == ".mx" {
		if err := compileFile(input, *outputDir, *runtimeDir, *dumpSSA); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	} else {
		if err := compilePackage(input, *outputDir, *runtimeDir, *dumpSSA); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	}
}

// compileFile compiles a single .mx file to JavaScript.
func compileFile(filename string, outputDir, runtimeDir string, dumpSSA bool) error {
	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, filename, nil, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("parse: %w", err)
	}

	conf := types.Config{
		Importer: importer.Default(),
	}
	info := &types.Info{
		Types:      make(map[ast.Expr]types.TypeAndValue),
		Defs:       make(map[*ast.Ident]types.Object),
		Uses:       make(map[*ast.Ident]types.Object),
		Instances:  make(map[*ast.Ident]types.Instance),
		Scopes:     make(map[ast.Node]*types.Scope),
		Selections: make(map[*ast.SelectorExpr]*types.Selection),
	}

	pkg, err := conf.Check("main", fset, []*ast.File{file}, info)
	if err != nil {
		return fmt.Errorf("typecheck: %w", err)
	}

	ssaProg := ssa.NewProgram(fset, ssa.BareInits|ssa.InstantiateGenerics)
	ssaProg.CreatePackage(pkg, []*ast.File{file}, info, true)

	packages := []jsbackend.PackageInfo{
		{Path: pkg.Path(), TypePkg: pkg},
	}

	config := &jsbackend.Config{
		OutputDir:  outputDir,
		RuntimeDir: runtimeDir,
		DumpSSA:    dumpSSA,
	}

	return jsbackend.CompileProgram(config, ssaProg, packages, "main")
}

// compilePackage compiles a Moxie package and its dependencies to JavaScript.
// Uses moxie-native package discovery (no go list, supports moxie.mod and .mx files).
func compilePackage(pattern string, outputDir, runtimeDir string, dumpSSA bool) error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}

	goroot := runtime.GOROOT()

	pkgJSONs, err := discoverPackages(goroot, wd, pattern)
	if err != nil {
		return fmt.Errorf("discover: %w", err)
	}
	if len(pkgJSONs) == 0 {
		return fmt.Errorf("no packages found for %q", pattern)
	}

	fset := token.NewFileSet()
	ssaProg := ssa.NewProgram(fset, ssa.BareInits|ssa.InstantiateGenerics)

	typePkgs := make(map[string]*types.Package)
	var allPkgs []jsbackend.PackageInfo

	for _, pj := range pkgJSONs {
		if pj.ImportPath == "unsafe" {
			typePkgs["unsafe"] = types.Unsafe
			ssaProg.CreatePackage(types.Unsafe, nil, nil, true)
			allPkgs = append(allPkgs, jsbackend.PackageInfo{
				Path:    "unsafe",
				TypePkg: types.Unsafe,
			})
			continue
		}

		var files []*ast.File
		for _, f := range pj.GoFiles {
			path := filepath.Join(pj.Dir, f)
			file, parseErr := parser.ParseFile(fset, path, nil, parser.ParseComments)
			if parseErr != nil {
				return fmt.Errorf("parse %s: %w", path, parseErr)
			}
			files = append(files, file)
		}

		conf := types.Config{
			Importer: &mapImporter{pkgs: typePkgs},
		}
		info := &types.Info{
			Types:      make(map[ast.Expr]types.TypeAndValue),
			Defs:       make(map[*ast.Ident]types.Object),
			Uses:       make(map[*ast.Ident]types.Object),
			Instances:  make(map[*ast.Ident]types.Instance),
			Scopes:     make(map[ast.Node]*types.Scope),
			Selections: make(map[*ast.SelectorExpr]*types.Selection),
		}

		typePkg, checkErr := conf.Check(pj.ImportPath, fset, files, info)
		if checkErr != nil {
			return fmt.Errorf("typecheck %s: %w", pj.ImportPath, checkErr)
		}
		typePkgs[pj.ImportPath] = typePkg

		ssaProg.CreatePackage(typePkg, files, info, pj.ImportPath == pattern || pj.Name == "main")

		allPkgs = append(allPkgs, jsbackend.PackageInfo{
			Path:    pj.ImportPath,
			TypePkg: typePkg,
		})
	}

	mainPkgPath := ""
	last := pkgJSONs[len(pkgJSONs)-1]
	if last.Name == "main" {
		mainPkgPath = last.ImportPath
	}

	config := &jsbackend.Config{
		OutputDir:  outputDir,
		RuntimeDir: runtimeDir,
		DumpSSA:    dumpSSA,
	}

	return jsbackend.CompileProgram(config, ssaProg, allPkgs, mainPkgPath)
}

// mapImporter resolves imports from already type-checked packages.
type mapImporter struct {
	pkgs map[string]*types.Package
}

func (m *mapImporter) Import(path string) (*types.Package, error) {
	if path == "unsafe" {
		return types.Unsafe, nil
	}
	if pkg, ok := m.pkgs[path]; ok {
		return pkg, nil
	}
	return nil, fmt.Errorf("package not imported: %s", path)
}
