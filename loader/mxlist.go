// mxlist.go implements package discovery for .mx source files,
// replacing the external `go list -json -deps` command.
//
// It uses go/build.Context with custom ReadDir/OpenFile hooks (from
// mxbuild.go) to find .mx files, then walks the dependency graph to
// produce the same PackageJSON structures the loader expects.

package loader

import (
	"fmt"
	"go/build"
	"os"
	"path/filepath"
	"strings"

	"moxie/compileopts"
	"moxie/goenv"
)

// mxListPackages discovers packages and their transitive dependencies,
// returning them in dependency order (dependencies before dependents).
// This replaces `go list -json -deps -e`.
func mxListPackages(config *compileopts.Config, goroot string, inputPkg string) ([]*PackageJSON, error) {
	return mxListPackagesImpl(config, goroot, inputPkg, false)
}

func mxListPackagesTest(config *compileopts.Config, goroot string, inputPkg string) ([]*PackageJSON, error) {
	return mxListPackagesImpl(config, goroot, inputPkg, true)
}

func mxListPackagesImpl(config *compileopts.Config, goroot string, inputPkg string, testMode bool) ([]*PackageJSON, error) {
	ctx := mxContext(goroot, config.GOOS(), config.GOARCH(), config.BuildTags())

	// Determine the working directory and module info.
	wd := config.Options.Directory
	if wd == "" {
		var err error
		wd, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	}

	modPath, modDir, modGoVersion, err := readModInfo(wd)
	if err != nil {
		return nil, fmt.Errorf("mxlist: %w", err)
	}

	// Read module cache path for external dependencies.
	modCache := goenv.Get("GOMODCACHE")
	if modCache == "" {
		gopath := goenv.Get("GOPATH")
		if gopath == "" {
			gopath = filepath.Join(os.Getenv("HOME"), "go")
		}
		modCache = filepath.Join(gopath, "pkg", "mod")
	}

	// Parse module require and replace directives.
	modFile := findModFile(modDir)
	requires, err := readModRequires(modFile)
	if err != nil {
		return nil, fmt.Errorf("mxlist: reading module requires: %w", err)
	}
	replaces, err := readModReplaces(modFile, modDir)
	if err != nil {
		return nil, fmt.Errorf("mxlist: reading module replaces: %w", err)
	}

	r := &resolver{
		ctx:          ctx,
		modPath:      modPath,
		modDir:       modDir,
		modGoVersion: modGoVersion,
		modCache:     modCache,
		requires:     requires,
		replaces:     replaces,
		goroot:       goroot,
		seen:         make(map[string]*PackageJSON),
	}

	// The runtime package is an implicit dependency — never explicitly
	// imported in user source, but always required by the compiler.
	// Walk it BEFORE the user package so that MainPkg() (which returns
	// the last element of sorted) correctly identifies the user package.
	if err := r.walk("runtime", ""); err != nil {
		return nil, err
	}

	// The moxie package is an implicit dependency — the compiler needs it
	// to look up the Codec interface for spawn-boundary type checking.
	// Like runtime, it lives in the synthetic GOROOT at src/moxie/.
	if err := r.walk("moxie", ""); err != nil {
		return nil, err
	}

	err = r.walk(inputPkg, wd)
	if err != nil {
		return nil, err
	}

	if testMode {
		sorted, err := r.addTestMain(inputPkg, wd)
		if err != nil {
			return nil, err
		}
		return sorted, nil
	}

	return r.sorted, nil
}

// MxListImportPaths returns just the import paths of the given packages,
// using the native .mx-aware resolver instead of `go list`.
func MxListImportPaths(config *compileopts.Config, pkgs []string) ([]string, error) {
	goroot, err := GetCachedGoroot(config)
	if err != nil {
		return nil, err
	}
	var result []string
	for _, pkg := range pkgs {
		pkgJSONs, err := mxListPackages(config, goroot, pkg)
		if err != nil {
			return nil, err
		}
		if len(pkgJSONs) > 0 {
			last := pkgJSONs[len(pkgJSONs)-1]
			result = append(result, last.ImportPath)
		}
	}
	return result, nil
}

// addTestMain finds _test.mx files in the target package, merges them,
// scans for Test* functions, generates a synthetic test main, and appends
// a .test main package to the resolver's sorted list.
func (r *resolver) addTestMain(inputPkg string, wd string) ([]*PackageJSON, error) {
	// Find the target package.
	var targetPkg *PackageJSON
	for _, pj := range r.sorted {
		if pj.ImportPath == inputPkg || (inputPkg == "." && pj.Dir == wd) {
			targetPkg = pj
			break
		}
	}
	if targetPkg == nil {
		return r.sorted, nil
	}

	// Find _test.mx files in the package directory.
	entries, err := os.ReadDir(targetPkg.Dir)
	if err != nil {
		return nil, err
	}
	var testFiles []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, "_test.mx") && !e.IsDir() {
			testFiles = append(testFiles, name)
		}
	}
	if len(testFiles) == 0 {
		return nil, NoTestFilesError{ImportPath: targetPkg.ImportPath}
	}

	// Merge test files into the target package.
	targetPkg.GoFiles = append(targetPkg.GoFiles, testFiles...)
	// Mark this package as the test package.
	targetPkg.ForTest = targetPkg.ImportPath

	// Remove target package from sorted — we need to re-add it after
	// walking test dependencies so the dependency order is correct.
	var targetIdx int
	for i, pj := range r.sorted {
		if pj == targetPkg {
			targetIdx = i
			break
		}
	}
	r.sorted = append(r.sorted[:targetIdx], r.sorted[targetIdx+1:]...)
	delete(r.seen, targetPkg.ImportPath)

	// Walk testing dependency (test files import "testing").
	if err := r.walk("testing", targetPkg.Dir); err != nil {
		return nil, fmt.Errorf("walking testing dependency: %w", err)
	}
	// Add "testing" to the package's imports.
	hasTestingImport := false
	for _, imp := range targetPkg.Imports {
		if imp == "testing" {
			hasTestingImport = true
			break
		}
	}
	if !hasTestingImport {
		targetPkg.Imports = append(targetPkg.Imports, "testing")
	}

	// Re-add target package after its test dependencies.
	r.seen[targetPkg.ImportPath] = targetPkg
	r.sorted = append(r.sorted, targetPkg)

	// Scan test files for Test* function names.
	var testFuncs []string
	for _, tf := range testFiles {
		data, err := os.ReadFile(filepath.Join(targetPkg.Dir, tf))
		if err != nil {
			return nil, err
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "func Test") {
				// Extract function name: "func TestFoo(t *testing.T) {"
				name := line[len("func "):]
				if idx := strings.IndexByte(name, '('); idx > 0 {
					name = name[:idx]
					testFuncs = append(testFuncs, name)
				}
			}
		}
	}

	// Generate synthetic _testmain.mx in a temp directory.
	tmpDir, err := os.MkdirTemp("", "moxie-testmain-*")
	if err != nil {
		return nil, err
	}

	var buf strings.Builder
	buf.WriteString("package main\n\nimport (\n")
	buf.WriteString("\t\"testing\"\n")
	buf.WriteString("\tpkg \"" + targetPkg.ImportPath + "\"\n")
	buf.WriteString(")\n\nfunc main() {\n")
	buf.WriteString("\ttests := []testing.InternalTest{\n")
	for _, fn := range testFuncs {
		buf.WriteString("\t\t{\"" + fn + "\", pkg." + fn + "},\n")
	}
	buf.WriteString("\t}\n")
	buf.WriteString("\ttesting.Main(tests)\n")
	buf.WriteString("}\n")

	mainFile := filepath.Join(tmpDir, "_testmain.mx")
	if err := os.WriteFile(mainFile, []byte(buf.String()), 0644); err != nil {
		return nil, err
	}

	// Add the .test main package.
	testMainPkg := &PackageJSON{
		Dir:        tmpDir,
		ImportPath: targetPkg.ImportPath + ".test",
		Name:       "main",
		GoFiles:    []string{"_testmain.mx"},
		Imports:    []string{"testing", targetPkg.ImportPath},
	}

	r.sorted = append(r.sorted, testMainPkg)
	return r.sorted, nil
}

// resolver walks the dependency graph, building PackageJSON entries.
type resolver struct {
	ctx          build.Context
	modPath      string
	modDir       string
	modGoVersion string
	modCache     string
	requires     map[string]string // module path → version
	replaces     map[string]string // module path → absolute local dir
	goroot       string
	seen         map[string]*PackageJSON
	sorted       []*PackageJSON
}

// walk imports a package and recursively walks its dependencies.
func (r *resolver) walk(importPath string, srcDir string) error {
	if r.seen[importPath] != nil {
		return nil
	}
	if importPath == "C" {
		// CGo pseudo-import, skip.
		return nil
	}
	if importPath == "unsafe" {
		// Built-in package, no files to discover.
		pj := &PackageJSON{
			ImportPath: "unsafe",
			Name:       "unsafe",
		}
		r.seen[importPath] = pj
		r.sorted = append(r.sorted, pj)
		return nil
	}

	// Mark as seen (with nil) to break import cycles.
	r.seen[importPath] = &PackageJSON{}

	dir, err := r.resolveDir(importPath, srcDir)
	if err != nil {
		return fmt.Errorf("resolving %q: %w", importPath, err)
	}

	pkg, err := r.ctx.ImportDir(dir, 0)
	if err != nil {
		// Check for NoGoError — might have only test files or only .mx files
		// that our context didn't find (shouldn't happen with our hooks).
		if _, ok := err.(*build.NoGoError); !ok {
			return fmt.Errorf("importing %q from %s: %w", importPath, dir, err)
		}
	}

	// Walk dependencies first (depth-first, deps before dependents).
	allImports := pkg.Imports
	for _, imp := range allImports {
		if err := r.walk(imp, dir); err != nil {
			return err
		}
	}

	// Build PackageJSON, mapping .go names back to .mx.
	pj := &PackageJSON{
		Dir:        dir,
		ImportPath: importPath,
		Name:       pkg.Name,
		GoFiles:    goToMx(pkg.GoFiles, dir),
		CgoFiles:   goToMx(pkg.CgoFiles, dir),
		CFiles:     pkg.CFiles,
		Imports:    pkg.Imports,
		EmbedFiles: pkg.EmbedPatterns,
	}

	// Set module info for packages within our module.
	if r.modPath != "" && (strings.HasPrefix(importPath, r.modPath+"/") || importPath == r.modPath) {
		pj.Module.Path = r.modPath
		pj.Module.Main = true
		pj.Module.Dir = r.modDir
		pj.Module.GoMod = findModFile(r.modDir)
		pj.Module.GoVersion = r.modGoVersion
	}

	r.seen[importPath] = pj
	r.sorted = append(r.sorted, pj)
	return nil
}

// resolveDir maps an import path to an on-disk directory.
func (r *resolver) resolveDir(importPath string, srcDir string) (string, error) {
	// 1. Local/relative imports.
	if importPath == "." || strings.HasPrefix(importPath, "./") || strings.HasPrefix(importPath, "../") {
		return filepath.Join(srcDir, importPath), nil
	}

	// 2. Standard library (in synthetic GOROOT).
	stdDir := filepath.Join(r.goroot, "src", importPath)
	if isDir(stdDir) {
		return stdDir, nil
	}
	// 2b. Stdlib-vendored packages (e.g. golang.org/x/crypto).
	stdVendorDir := filepath.Join(r.goroot, "src", "vendor", importPath)
	if isDir(stdVendorDir) {
		return stdVendorDir, nil
	}

	// 3. Module-local packages.
	if r.modPath != "" && (strings.HasPrefix(importPath, r.modPath+"/") || importPath == r.modPath) {
		rel := strings.TrimPrefix(importPath, r.modPath)
		rel = strings.TrimPrefix(rel, "/")
		dir := filepath.Join(r.modDir, rel)
		if isDir(dir) {
			return dir, nil
		}
		return "", fmt.Errorf("package %q not found in module at %s", importPath, dir)
	}

	// 4a. Local replace directives.
	for mod, localDir := range r.replaces {
		if importPath == mod || strings.HasPrefix(importPath, mod+"/") {
			rel := strings.TrimPrefix(importPath, mod)
			rel = strings.TrimPrefix(rel, "/")
			dir := filepath.Join(localDir, rel)
			if isDir(dir) {
				return dir, nil
			}
			return "", fmt.Errorf("package %q not found in replace at %s", importPath, dir)
		}
	}

	// 4b. External dependencies (module cache).
	// Find the longest matching module path from go.mod requires.
	bestMod := ""
	bestVer := ""
	for mod, ver := range r.requires {
		if (importPath == mod || strings.HasPrefix(importPath, mod+"/")) && len(mod) > len(bestMod) {
			bestMod = mod
			bestVer = ver
		}
	}
	if bestMod != "" {
		// Module cache path: $GOMODCACHE/module@version/subpath
		modCacheDir := filepath.Join(r.modCache, bestMod+"@"+bestVer)
		rel := strings.TrimPrefix(importPath, bestMod)
		rel = strings.TrimPrefix(rel, "/")
		dir := filepath.Join(modCacheDir, rel)
		if isDir(dir) {
			return dir, nil
		}
		return "", fmt.Errorf("package %q not found in module cache at %s", importPath, dir)
	}

	// 5. Vendored packages.
	if r.modDir != "" {
		vendorDir := filepath.Join(r.modDir, "vendor", importPath)
		if isDir(vendorDir) {
			return vendorDir, nil
		}
	}

	return "", fmt.Errorf("package %q not found (not in stdlib, module, or cache)", importPath)
}

// findModFile returns the path of the module file in dir.
// Prefers moxie.mod, falls back to go.mod.
func findModFile(dir string) string {
	mxmod := filepath.Join(dir, "moxie.mod")
	if _, err := os.Stat(mxmod); err == nil {
		return mxmod
	}
	return filepath.Join(dir, "go.mod")
}

// readModInfo reads the module path and directory from moxie.mod (or go.mod).
func readModInfo(startDir string) (modPath, modDir, goVersion string, err error) {
	dir := startDir
	for {
		modFile := findModFile(dir)
		data, err := os.ReadFile(modFile)
		if err == nil {
			modPath, goVersion = parseGoMod(string(data))
			return modPath, dir, goVersion, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", startDir, "", nil // no module file found, use wd
		}
		dir = parent
	}
}

// parseGoMod extracts the module path and go version from go.mod content.
func parseGoMod(content string) (modPath, goVersion string) {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			modPath = strings.TrimSpace(strings.TrimPrefix(line, "module"))
		}
		if strings.HasPrefix(line, "go ") {
			goVersion = strings.TrimSpace(strings.TrimPrefix(line, "go"))
		}
	}
	return
}

// readModRequires parses require directives from go.mod.
func readModRequires(gomodPath string) (map[string]string, error) {
	data, err := os.ReadFile(gomodPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	requires := make(map[string]string)
	inRequire := false
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)

		if line == ")" {
			inRequire = false
			continue
		}
		if strings.HasPrefix(line, "require (") {
			inRequire = true
			continue
		}
		if strings.HasPrefix(line, "require ") && !strings.Contains(line, "(") {
			// Single-line require.
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				requires[parts[1]] = parts[2]
			}
			continue
		}
		if inRequire {
			parts := strings.Fields(line)
			if len(parts) >= 2 && !strings.HasPrefix(parts[0], "//") {
				requires[parts[0]] = parts[1]
			}
		}
	}
	return requires, nil
}

// readModReplaces parses replace directives that point to local directories.
func readModReplaces(gomodPath, modDir string) (map[string]string, error) {
	data, err := os.ReadFile(gomodPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	replaces := make(map[string]string)
	inReplace := false
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == ")" {
			inReplace = false
			continue
		}
		if strings.HasPrefix(line, "replace (") {
			inReplace = true
			continue
		}
		parseLine := ""
		if strings.HasPrefix(line, "replace ") && !strings.Contains(line, "(") {
			parseLine = strings.TrimPrefix(line, "replace ")
		} else if inReplace && !strings.HasPrefix(line, "//") && line != "" {
			parseLine = line
		}
		if parseLine == "" {
			continue
		}
		parts := strings.Split(parseLine, "=>")
		if len(parts) != 2 {
			continue
		}
		lhs := strings.Fields(strings.TrimSpace(parts[0]))
		rhs := strings.Fields(strings.TrimSpace(parts[1]))
		if len(lhs) < 1 || len(rhs) < 1 {
			continue
		}
		replacePath := rhs[0]
		if strings.HasPrefix(replacePath, ".") || strings.HasPrefix(replacePath, "/") {
			if !filepath.IsAbs(replacePath) {
				replacePath = filepath.Join(modDir, replacePath)
			}
			replaces[lhs[0]] = filepath.Clean(replacePath)
		}
	}
	return replaces, nil
}

func isDir(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}
