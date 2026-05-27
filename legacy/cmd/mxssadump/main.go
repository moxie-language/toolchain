// mxssadump exercises the mxssa builder on a real package.
// It loads a package via the moxie loader, runs the native type checker (B1),
// then builds the mxssa IR (B2) and prints a summary.
//
// Usage: mxssadump <import-path|.>
package main

import (
	"fmt"
	"go/token"
	"go/types"
	"os"
	"runtime"
	"sort"
	"strings"

	"moxie/compileopts"
	"moxie/goenv"
	"moxie/loader"
	"moxie/mxssa"
	"moxie/syntax"
	"moxie/typecheck"
	"moxie/typecheck/bridge"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: mxssadump <import-path|.>")
		os.Exit(1)
	}
	target := os.Args[1]

	opts := &compileopts.Options{GOOS: "linux", GOARCH: "amd64"}
	cfg, err := newConfig(opts)
	if err != nil {
		fatalf("config: %v", err)
	}

	typeCfg := types.Config{Error: func(e error) {}}
	prog, err := loader.Load(cfg, target, typeCfg)
	if err != nil {
		fatalf("load: %v", err)
	}
	prog.EnableSyntaxFiles()
	if err := prog.Parse(); err != nil {
		if !isMainNameError(err.Error()) {
			fatalf("parse: %v", err)
		}
	}

	mainPkg := prog.MainPkg()
	if mainPkg == nil || len(mainPkg.SyntaxFiles) == 0 {
		fatalf("no syntax files for %s", target)
	}

	// Build bridge importer from the go/types package graph.
	pkgMap := make(map[string]*types.Package, len(prog.Packages))
	for path, p := range prog.Packages {
		if p.Pkg != nil {
			pkgMap[path] = p.Pkg
		}
	}
	imp := bridge.New(pkgMap)

	// Run the native checker (B1).
	tcPkg, _, err := typecheck.Check(mainPkg.ImportPath, mainPkg.SyntaxFiles, imp)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warn: typecheck: %v\n", err)
	}

	// Build the mxssa IR (B2).
	fset := token.NewFileSet()
	ssaProg := mxssa.NewProgram(fset)

	if tcPkg == nil {
		fatalf("typecheck returned nil package")
	}

	// Collect typecheck.Info from the checker.
	_, info, _ := typecheck.Check(mainPkg.ImportPath, mainPkg.SyntaxFiles, imp)

	ssaPkg := ssaProg.CreatePackage(tcPkg, mainPkg.SyntaxFiles, info)

	// Print summary.
	fmt.Printf("package %s\n", mainPkg.ImportPath)
	fmt.Printf("  syntax files:  %d\n", len(mainPkg.SyntaxFiles))
	fmt.Printf("  ssa members:   %d\n", len(ssaPkg.Members))

	var funcs, globals, types_, consts int
	var funcNames []string
	for name, m := range ssaPkg.Members {
		switch m.(type) {
		case *mxssa.Function:
			funcs++
			funcNames = append(funcNames, name)
		case *mxssa.Global:
			globals++
		case *mxssa.Type_:
			types_++
		case *mxssa.NamedConst:
			consts++
		}
	}
	sort.Strings(funcNames)
	fmt.Printf("  functions:     %d\n", funcs)
	fmt.Printf("  globals:       %d\n", globals)
	fmt.Printf("  types:         %d\n", types_)
	fmt.Printf("  consts:        %d\n", consts)

	// Print function block counts.
	fmt.Println("\nfunctions:")
	for _, name := range funcNames {
		fn := ssaPkg.Members[name].(*mxssa.Function)
		fmt.Printf("  %-30s  blocks=%d params=%d\n", name, len(fn.Blocks), len(fn.Params))
	}
}

func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "mxssadump: "+format+"\n", args...)
	os.Exit(1)
}

func newConfig(options *compileopts.Options) (*compileopts.Config, error) {
	spec, err := compileopts.LoadTarget(options)
	if err != nil {
		return nil, err
	}
	_, gorootMinor, err := goenv.GetGorootVersion()
	if err != nil {
		return nil, err
	}
	_, buildMinor, _, err := goenv.Parse(runtime.Version())
	if err != nil {
		return nil, err
	}
	if buildMinor < gorootMinor {
		return nil, fmt.Errorf("go toolchain mismatch: built with %s, running %d.%d",
			runtime.Version(), 1, gorootMinor)
	}
	return &compileopts.Config{
		Options:        options,
		Target:         spec,
		GoMinorVersion: gorootMinor,
		TestConfig:     options.TestConfig,
	}, nil
}

func isMainNameError(msg string) bool {
	return strings.Contains(msg, "expected main package to have name")
}

var _ []*syntax.File
