// tinyjs compiles Go source to JavaScript ES modules.
//
// Usage:
//
//	tinyjs [-o outputdir] [-runtime runtimedir] [-dump-ssa] <package>
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"

	"moxie/jsbackend"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

func main() {
	outputDir := flag.String("o", "jsout", "output directory for .mjs files")
	runtimeDir := flag.String("runtime", "", "path to jsruntime/ directory (if empty, must be copied manually)")
	dumpSSA := flag.Bool("dump-ssa", false, "dump SSA for debugging")
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: tinyjs [-o dir] <file.go or package path>")
		os.Exit(1)
	}

	input := args[0]

	// Detect if input is a .go file or a package path.
	if filepath.Ext(input) == ".go" {
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

// compileFile compiles a single .go file to JavaScript.
func compileFile(filename string, outputDir, runtimeDir string, dumpSSA bool) error {
	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, filename, nil, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("parse: %w", err)
	}

	// Type check.
	conf := types.Config{
		Importer: newImporter(),
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

	// Build SSA.
	ssaProg := ssa.NewProgram(fset, ssa.BareInits|ssa.InstantiateGenerics)
	ssaPkg := ssaProg.CreatePackage(pkg, []*ast.File{file}, info, true)
	_ = ssaPkg

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

// compilePackage compiles a Go package to JavaScript using go/packages.
func compilePackage(pattern string, outputDir, runtimeDir string, dumpSSA bool) error {
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax |
			packages.NeedTypes | packages.NeedTypesInfo | packages.NeedDeps |
			packages.NeedImports,
		Fset: token.NewFileSet(),
	}

	pkgs, err := packages.Load(cfg, pattern)
	if err != nil {
		return fmt.Errorf("load: %w", err)
	}

	if len(pkgs) == 0 {
		return fmt.Errorf("no packages found for %q", pattern)
	}

	// Check for errors.
	for _, pkg := range pkgs {
		for _, e := range pkg.Errors {
			fmt.Fprintf(os.Stderr, "%s\n", e)
		}
	}

	// Build SSA from loaded packages.
	ssaProg, ssaPkgs := ssautil.AllPackages(pkgs, ssa.BareInits|ssa.InstantiateGenerics)
	_ = ssaPkgs

	// Collect all packages in dependency order.
	var allPkgs []jsbackend.PackageInfo
	seen := make(map[string]bool)
	for _, pkg := range pkgs {
		collectDeps(pkg, &allPkgs, seen)
	}

	mainPkgPath := ""
	if pkgs[0].Name == "main" {
		mainPkgPath = pkgs[0].PkgPath
	}

	config := &jsbackend.Config{
		OutputDir:  outputDir,
		RuntimeDir: runtimeDir,
		DumpSSA:    dumpSSA,
	}

	return jsbackend.CompileProgram(config, ssaProg, allPkgs, mainPkgPath)
}

func collectDeps(pkg *packages.Package, result *[]jsbackend.PackageInfo, seen map[string]bool) {
	if seen[pkg.PkgPath] {
		return
	}
	seen[pkg.PkgPath] = true

	// Process deps first (dependency order).
	depPaths := make([]string, 0, len(pkg.Imports))
	for path := range pkg.Imports {
		depPaths = append(depPaths, path)
	}
	sort.Strings(depPaths)
	for _, path := range depPaths {
		collectDeps(pkg.Imports[path], result, seen)
	}

	if pkg.Types != nil {
		*result = append(*result, jsbackend.PackageInfo{
			Path:    pkg.PkgPath,
			TypePkg: pkg.Types,
		})
	}
}

// newImporter returns a types.Importer that uses go/packages.
func newImporter() types.Importer {
	return &goImporter{cache: make(map[string]*types.Package)}
}

type goImporter struct {
	cache map[string]*types.Package
}

func (imp *goImporter) Import(path string) (*types.Package, error) {
	if pkg, ok := imp.cache[path]; ok {
		return pkg, nil
	}

	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax |
			packages.NeedTypes | packages.NeedTypesInfo,
	}

	pkgs, err := packages.Load(cfg, path)
	if err != nil {
		return nil, err
	}
	if len(pkgs) == 0 {
		return nil, fmt.Errorf("package %q not found", path)
	}
	if pkgs[0].Types == nil {
		return nil, fmt.Errorf("package %q has no type info", path)
	}

	imp.cache[path] = pkgs[0].Types
	return pkgs[0].Types, nil
}
