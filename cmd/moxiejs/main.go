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
	"moxie/mxtext"
	"golang.org/x/tools/go/ssa"
)

// readAndRewrite reads a source file and applies moxie literal rewrites.
// For .mx files, it rewrites moxie-specific syntax (slice/chan literals)
// to standard Go before parsing. For .go files, returns content as-is.
func readAndRewrite(path string) ([]byte, error) {
	// discover.go maps .go filenames to .mx on disk
	actual := path
	if filepath.Ext(path) == ".go" {
		mxPath := path[:len(path)-3] + ".mx"
		if _, err := os.Stat(mxPath); err == nil {
			actual = mxPath
		}
	}
	src, err := os.ReadFile(actual)
	if err != nil {
		return nil, err
	}
	if filepath.Ext(actual) == ".mx" {
		src = rewriteMoxieLiterals(src)
	}
	return src, nil
}

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

	src, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("read %s: %w", filename, err)
	}
	src = rewriteMoxieLiterals(src)

	file, err := parser.ParseFile(fset, filename, src, parser.ParseComments)
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
		Implicits:  make(map[ast.Node]types.Object),
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

	// Derive moxie stdlib src dir from the -runtime flag.
	// -runtime points to e.g. ../moxie/jsruntime/, so ../src/ is the stdlib.
	var moxieSrcDir string
	if runtimeDir != "" {
		candidate := filepath.Join(runtimeDir, "..", "src")
		if abs, err := filepath.Abs(candidate); err == nil {
			if info, err := os.Stat(abs); err == nil && info.IsDir() {
				moxieSrcDir = abs
			}
		}
	}
	if moxieSrcDir == "" {
		if root := os.Getenv("MOXIEROOT"); root != "" {
			candidate := filepath.Join(root, "src")
			if info, err := os.Stat(candidate); err == nil && info.IsDir() {
				moxieSrcDir = candidate
			}
		}
	}

	pkgJSONs, err := discoverPackages(goroot, wd, pattern, moxieSrcDir)
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
			src, readErr := readAndRewrite(path)
			if readErr != nil {
				return fmt.Errorf("read %s: %w", path, readErr)
			}
			file, parseErr := parser.ParseFile(fset, path, src, parser.ParseComments)
			if parseErr != nil {
				return fmt.Errorf("parse %s: %w", path, parseErr)
			}
			// Apply AST-level string→[]byte rewrites for Moxie-target packages.
			// This is what native moxie does via patched go/types; moxiejs instead
			// transforms the AST so the standard go/types sees []byte throughout.
			// Exempt packages (runtime, syscall, os, reflect, unsafe, internal/abi,
			// internal/task, internal/reflectlite, internal/itoa) keep native Go
			// string types because they interact with low-level primitives.
			if mxtext.IsMoxieStringTarget(pj.ImportPath) {
				mxtext.RewriteStringTypes(file)
				mxtext.RewriteStringLiterals(file)
				mxtext.RewriteStringConversions(file)
				// MUST run after RewriteStringConversions so the string(...)
				// return wraps it introduces aren't immediately reverted to
				// []byte(...).
				mxtext.WrapInterfaceMandatedReturns(file)
				// Convert text concatenations (ADD/OR with syntactically-text
				// operands) into __moxie_concat(...) calls so the standard
				// go/types accepts them. Native moxie uses patched go/types
				// that permits []byte + []byte; moxiejs rewrites instead.
				mxtext.RewriteTextConcat(file)
				// Swap unsafe.String(ptr,len) → unsafe.Slice(ptr,len). In
				// Moxie string==[]byte, so for *byte callers the two are
				// identical; stock go/types otherwise rejects returning a
				// string where []byte is expected.
				mxtext.RewriteUnsafeString(file)
			} else {
				// Exempt packages (syscall, os, runtime, reflect, ...) keep
				// native Go string types. But their calls into non-exempt
				// packages (e.g. errors.New) must match rewritten []byte
				// signatures. Wrap string literals at those call sites.
				mxtext.RewriteExemptCrossBoundaryCalls(file)
			}
			files = append(files, file)
		}
		// Inject the __moxie_concat/eq/lt/secalloc builtin declarations.
		// Non-exempt packages always need them for pipe concat and comparisons.
		// Exempt packages also need them when they interact with non-exempt
		// interface methods that were rewritten to []byte (e.g. os sorting by
		// io/fs.DirEntry.Name()).
		pkgName := ""
		if len(files) > 0 {
			pkgName = files[0].Name.Name
		}
		if pkgName != "" && pkgName != "unsafe" {
			if bi := mxtext.InjectMoxieByteBuiltins(fset, pkgName); bi != nil {
				files = append(files, bi)
			}
		}

		typePkg, info, typeErr := checkPackage(pj, files, fset, typePkgs)
		if typeErr != nil {
			return fmt.Errorf("typecheck %s: %w", pj.ImportPath, typeErr)
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

// checkPackage type-checks pj with a Moxie-target two-pass dance:
//
//   1. First pass: Error-callback collects type errors so go/types keeps
//      walking. For non-text packages the errors are final; for Moxie-target
//      packages they feed into step 2.
//   2. AST-level rewrite: mxtext.FindByteComparisons / FindByteSwitches /
//      FindPipeConcat / RewriteAddAssign use the (partial) type info to
//      rewrite remaining text operations that the pre-parse syntactic pass
//      couldn't detect (e.g. `name == mid` where both sides are plain
//      Idents resolving to []byte).
//   3. Second pass: fresh info, re-typecheck. If errors remain, return the
//      first.
//
// Returns (typePkg, info, err). On pure-Go (exempt) packages only one pass
// runs.
func checkPackage(pj *packageJSON, files []*ast.File, fset *token.FileSet, typePkgs map[string]*types.Package) (*types.Package, *types.Info, error) {
	makeInfo := func() *types.Info {
		return &types.Info{
			Types:      make(map[ast.Expr]types.TypeAndValue),
			Defs:       make(map[*ast.Ident]types.Object),
			Uses:       make(map[*ast.Ident]types.Object),
			Instances:  make(map[*ast.Ident]types.Instance),
			Implicits:  make(map[ast.Node]types.Object),
			Scopes:     make(map[ast.Node]*types.Scope),
			Selections: make(map[*ast.SelectorExpr]*types.Selection),
		}
	}

	info := makeInfo()
	var typeErrors []error
	conf := types.Config{
		Importer: &mapImporter{pkgs: typePkgs},
		Error: func(err error) {
			typeErrors = append(typeErrors, err)
		},
	}
	typePkg, _ := conf.Check(pj.ImportPath, fset, files, info)

	if !mxtext.IsMoxieStringTarget(pj.ImportPath) {
		// Exempt-package phase 2: use type info to wrap string-typed args
		// at calls into non-exempt packages (whose params are []byte), plus
		// byte-comparison rewrites for when non-exempt interfaces (e.g.
		// io/fs.DirEntry.Name() []byte) bleed []byte into exempt code.
		// Do NOT run FindPipeConcat here — it rewrites `string+string` which
		// is perfectly valid in exempt (native Go) packages.
		mismatches := mxtext.FindExemptCrossBoundaryMismatches(files, info)
		cmpExprs := mxtext.FindByteComparisons(files, info)
		structLits := mxtext.FindExemptStructLiteralMismatches(files, info)
		if len(mismatches) == 0 && len(cmpExprs) == 0 && len(structLits) == 0 {
			if len(typeErrors) > 0 {
				return nil, nil, typeErrors[0]
			}
			return typePkg, info, nil
		}
		mxtext.ApplyExemptCrossBoundaryMismatches(files, mismatches)
		mxtext.ApplyByteComparisonRewrites(files, cmpExprs)
		mxtext.ApplyExemptStructLiteralMismatches(files, structLits)
		info = makeInfo()
		typeErrors = nil
		typePkg, _ = conf.Check(pj.ImportPath, fset, files, info)
		if len(typeErrors) > 0 {
			return nil, nil, typeErrors[0]
		}
		return typePkg, info, nil
	}

	// Moxie-target: const-pipe rewrite is syntactic, one-shot.
	mxtext.RewriteConstPipes(files)
	// Phase-2 loop: find + apply rewrites using (partial) type info, then
	// re-typecheck. Iterate until either no rewrites are produced or the
	// typecheck is error-free. Some rewrites (e.g. pipe→__moxie_concat)
	// change an expression's type, which unlocks mismatches that were not
	// visible in the prior pass (e.g. string map key indexed with a pipe
	// result that was string pre-rewrite but becomes []byte after).
	const maxPasses = 6
	for pass := 0; pass < maxPasses; pass++ {
		pipeRewrites := mxtext.FindPipeConcat(files, info)
		cmpExprs := mxtext.FindByteComparisons(files, info)
		byteSwitches := mxtext.FindByteSwitches(files, info)
		mapKeys := mxtext.FindByteMapKeys(files, info)
		addAssignCount := mxtext.RewriteAddAssign(files, info)
		nonExemptMismatches := mxtext.FindNonExemptCrossBoundaryMismatches(files, info)
		returnMismatches := mxtext.FindNonExemptReturnMismatches(files, info)
		byteConvs := mxtext.FindByteToSliceConversions(files, info)
		structLits := mxtext.FindExemptStructLiteralMismatches(files, info)
		assignMismatches := mxtext.FindNonExemptAssignMismatches(files, info)

		total := len(pipeRewrites) + len(cmpExprs) + len(byteSwitches) + len(mapKeys) + addAssignCount + len(nonExemptMismatches) + len(returnMismatches) + len(byteConvs) + len(structLits) + len(assignMismatches)
		if os.Getenv("MOXIE_DEBUG_PASSES") != "" {
			fmt.Fprintf(os.Stderr, "[pass %d pkg=%s] pipes=%d cmp=%d switches=%d mapKeys=%d addAssign=%d nonExempt=%d returns=%d byteConvs=%d structLits=%d assignMm=%d errors=%d\n",
				pass, pj.ImportPath, len(pipeRewrites), len(cmpExprs), len(byteSwitches), len(mapKeys), addAssignCount, len(nonExemptMismatches), len(returnMismatches), len(byteConvs), len(structLits), len(assignMismatches), len(typeErrors))
			for i, err := range typeErrors {
				if i >= 3 {
					break
				}
				fmt.Fprintf(os.Stderr, "  err[%d]: %v\n", i, err)
			}
		}
		if total == 0 {
			if len(typeErrors) > 0 {
				return nil, nil, typeErrors[0]
			}
			return typePkg, info, nil
		}

		mxtext.ApplyPipeRewrites(files, pipeRewrites)
		mxtext.ApplyByteComparisonRewrites(files, cmpExprs)
		mxtext.ApplyByteSwitchRewrites(byteSwitches)
		mxtext.ApplyByteMapKeyRewrites(mapKeys)
		mxtext.ApplyNonExemptCrossBoundaryMismatches(files, nonExemptMismatches)
		mxtext.ApplyNonExemptReturnMismatches(files, returnMismatches)
		mxtext.ApplyByteToSliceConversions(files, byteConvs)
		mxtext.ApplyExemptStructLiteralMismatches(files, structLits)
		mxtext.ApplyNonExemptAssignMismatches(files, assignMismatches)

		info = makeInfo()
		typeErrors = nil
		typePkg, _ = conf.Check(pj.ImportPath, fset, files, info)
	}
	if len(typeErrors) > 0 {
		return nil, nil, typeErrors[0]
	}
	return typePkg, info, nil
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
