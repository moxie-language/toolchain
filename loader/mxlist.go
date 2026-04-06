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
