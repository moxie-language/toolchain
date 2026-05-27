// mxcheck validates the native Moxie type checker (B1) against real packages.
// It loads a package through the moxie loader (same pipeline as the compiler),
// runs the native checker on the syntax files, and diffs against go/types.
//
// Usage: mxcheck [-compare] <import-path>
package main

import (
	"flag"
	"fmt"
	"go/types"
	"os"
	"sort"
	"strings"

	"moxie/compileopts"
	"moxie/goenv"
	"moxie/loader"
	"moxie/syntax"
	"moxie/typecheck"
	"moxie/typecheck/bridge"
	"runtime"
)

var flagCompare = flag.Bool("compare", false, "compare native errors against go/types")

func main() {
	flag.Parse()
	if flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: mxcheck [-compare] <import-path|.>")
		os.Exit(1)
	}
	target := flag.Arg(0)

	// Load via the moxie loader so we get the full package graph,
	// including rewrites (chan literals, slice literals, string literals).
	opts := &compileopts.Options{
		GOOS:   "linux",
		GOARCH: "amd64",
	}
	cfg, err := newConfig(opts)
	if err != nil {
		fatalf("config: %v", err)
	}

	var goTypeErrors []string
	typeCfg := types.Config{
		Error: func(e error) {
			goTypeErrors = append(goTypeErrors, normaliseError(e.Error()))
		},
	}

	prog, err := loader.Load(cfg, target, typeCfg)
	if err != nil {
		fatalf("load: %v", err)
	}
	prog.EnableSyntaxFiles()
	if err := prog.Parse(); err != nil {
		// The loader enforces that the target package is named "main" for
		// compilation. For library packages (fmt, bytes, moxie/typecheck, etc.)
		// this check fails, but all dependencies are still parsed and
		// type-checked. We ignore this specific error so mxcheck can operate
		// on library packages.
		if !isMainNameError(err.Error()) {
			fatalf("parse: %v", err)
		}
	}

	mainPkg := prog.MainPkg()

	// Build bridge importer from the full package graph.
	pkgMap := make(map[string]*types.Package, len(prog.Packages))
	for path, p := range prog.Packages {
		if p.Pkg != nil {
			pkgMap[path] = p.Pkg
		}
	}
	imp := bridge.New(pkgMap)

	// Run the native checker on the main package's syntax files.
	syntaxFiles := mainPkg.SyntaxFiles
	if len(syntaxFiles) == 0 {
		fmt.Println("no syntax files for", mainPkg.ImportPath)
		os.Exit(1)
	}

	_, _, nativeErr := typecheck.Check(mainPkg.ImportPath, syntaxFiles, imp)

	if *flagCompare {
		sort.Strings(goTypeErrors)
		var nativeErrs []string
		if nativeErr != nil {
			nativeErrs = append(nativeErrs, normaliseError(nativeErr.Error()))
		}
		sort.Strings(nativeErrs)
		compare(goTypeErrors, nativeErrs, mainPkg.ImportPath)
		return
	}

	if nativeErr != nil {
		fmt.Fprintln(os.Stderr, nativeErr)
		os.Exit(1)
	}
	fmt.Printf("ok\t%s\t(%d files)\n", mainPkg.ImportPath, len(syntaxFiles))
}

func compare(goErrs, nativeErrs []string, pkgPath string) {
	fmt.Printf("%-40s  go/types: %d  native: %d\n", pkgPath, len(goErrs), len(nativeErrs))

	onlyGo := setDiff(goErrs, nativeErrs)
	onlyNative := setDiff(nativeErrs, goErrs)

	if len(onlyGo) == 0 && len(onlyNative) == 0 {
		fmt.Println("PASS")
		return
	}
	for _, e := range onlyGo {
		fmt.Println("  - go/types:", e)
	}
	for _, e := range onlyNative {
		fmt.Println("  + native: ", e)
	}
	os.Exit(1)
}

func setDiff(a, b []string) []string {
	set := make(map[string]bool, len(b))
	for _, s := range b {
		set[s] = true
	}
	var out []string
	for _, s := range a {
		if !set[s] {
			out = append(out, s)
		}
	}
	return out
}

func normaliseError(e string) string {
	if i := strings.Index(e, ": "); i >= 0 {
		return strings.TrimSpace(e[i+2:])
	}
	return strings.TrimSpace(e)
}

func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "mxcheck: "+format+"\n", args...)
	os.Exit(1)
}

// newConfig builds a compileopts.Config without importing moxie/builder
// (which would drag in LLVM). Mirrors builder.NewConfig exactly.
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

// Keep syntax imported for the SyntaxFiles type.
var _ []*syntax.File
