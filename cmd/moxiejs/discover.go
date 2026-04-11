// discover.go — Moxie-native package discovery for moxiejs.
// Finds .mx packages and dependencies without go/packages (which
// requires `go list` and a go.mod file). Supports moxie.mod.

package main

import (
	"fmt"
	"go/build"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// packageJSON holds package metadata for a discovered package.
type packageJSON struct {
	Dir        string
	ImportPath string
	Name       string
	Module     struct {
		Path      string
		Main      bool
		Dir       string
		GoMod     string
		GoVersion string
	}
	GoFiles  []string
	CgoFiles []string
	CFiles   []string
	Imports  []string
}

// discoverPackages finds all packages transitively imported by inputPkg,
// returning them in dependency order (deps before dependents).
func discoverPackages(goroot, workDir, inputPkg string) ([]*packageJSON, error) {
	ctx := mxBuildContext(goroot)

	modPath, modDir, modGoVersion, err := readModInfo(workDir)
	if err != nil {
		return nil, fmt.Errorf("discover: %w", err)
	}

	modCache := os.Getenv("GOMODCACHE")
	if modCache == "" {
		gopath := os.Getenv("GOPATH")
		if gopath == "" {
			gopath = filepath.Join(os.Getenv("HOME"), "go")
		}
		modCache = filepath.Join(gopath, "pkg", "mod")
	}

	modFile := findModFile(modDir)
	requires, err := readModRequires(modFile)
	if err != nil {
		return nil, fmt.Errorf("discover: reading module requires: %w", err)
	}

	replaces, err := readModReplaces(modFile, modDir)
	if err != nil {
		return nil, fmt.Errorf("discover: reading module replaces: %w", err)
	}

	r := &pkgResolver{
		ctx:          ctx,
		modPath:      modPath,
		modDir:       modDir,
		modGoVersion: modGoVersion,
		modCache:     modCache,
		requires:     requires,
		replaces:     replaces,
		goroot:       goroot,
		seen:         make(map[string]*packageJSON),
	}

	if err := r.walk(inputPkg, workDir); err != nil {
		return nil, err
	}

	return r.sorted, nil
}

type pkgResolver struct {
	ctx          build.Context
	modPath      string
	modDir       string
	modGoVersion string
	modCache     string
	requires     map[string]string
	replaces     map[string]string // module path → absolute local dir
	goroot       string
	seen         map[string]*packageJSON
	sorted       []*packageJSON
}

func (r *pkgResolver) walk(importPath, srcDir string) error {
	if r.seen[importPath] != nil {
		return nil
	}
	if importPath == "C" {
		return nil
	}
	if importPath == "unsafe" {
		pj := &packageJSON{ImportPath: "unsafe", Name: "unsafe"}
		r.seen[importPath] = pj
		r.sorted = append(r.sorted, pj)
		return nil
	}

	r.seen[importPath] = &packageJSON{} // cycle breaker

	dir, err := r.resolveDir(importPath, srcDir)
	if err != nil {
		return fmt.Errorf("resolving %q: %w", importPath, err)
	}

	pkg, err := r.ctx.ImportDir(dir, 0)
	if err != nil {
		if _, ok := err.(*build.NoGoError); !ok {
			return fmt.Errorf("importing %q from %s: %w", importPath, dir, err)
		}
	}

	for _, imp := range pkg.Imports {
		if err := r.walk(imp, dir); err != nil {
			return err
		}
	}

	pj := &packageJSON{
		Dir:        dir,
		ImportPath: importPath,
		Name:       pkg.Name,
		GoFiles:    goToMx(pkg.GoFiles, dir),
		CgoFiles:   goToMx(pkg.CgoFiles, dir),
		CFiles:     pkg.CFiles,
		Imports:    pkg.Imports,
	}

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

func (r *pkgResolver) resolveDir(importPath, srcDir string) (string, error) {
	if importPath == "." || strings.HasPrefix(importPath, "./") || strings.HasPrefix(importPath, "../") {
		return filepath.Join(srcDir, importPath), nil
	}

	stdDir := filepath.Join(r.goroot, "src", importPath)
	if isDir(stdDir) {
		return stdDir, nil
	}
	stdVendorDir := filepath.Join(r.goroot, "src", "vendor", importPath)
	if isDir(stdVendorDir) {
		return stdVendorDir, nil
	}

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
	bestMod := ""
	bestVer := ""
	for mod, ver := range r.requires {
		if (importPath == mod || strings.HasPrefix(importPath, mod+"/")) && len(mod) > len(bestMod) {
			bestMod = mod
			bestVer = ver
		}
	}
	if bestMod != "" {
		modCacheDir := filepath.Join(r.modCache, bestMod+"@"+bestVer)
		rel := strings.TrimPrefix(importPath, bestMod)
		rel = strings.TrimPrefix(rel, "/")
		dir := filepath.Join(modCacheDir, rel)
		if isDir(dir) {
			return dir, nil
		}
		return "", fmt.Errorf("package %q not found in module cache at %s", importPath, dir)
	}

	if r.modDir != "" {
		vendorDir := filepath.Join(r.modDir, "vendor", importPath)
		if isDir(vendorDir) {
			return vendorDir, nil
		}
	}

	return "", fmt.Errorf("package %q not found (not in stdlib, module, or cache)", importPath)
}

// --- Build context hooks (present .mx as .go to go/build) ---

func mxBuildContext(goroot string) build.Context {
	ctx := build.Default
	ctx.GOROOT = goroot
	ctx.GOOS = "js"
	ctx.GOARCH = "wasm"
	ctx.CgoEnabled = false
	ctx.ReadDir = mxReadDir
	ctx.OpenFile = mxOpenFile
	// Strip host goexperiment.* tags. The host Go toolchain injects tags
	// reflecting how *it* was built (regabiargs, regabiwrappers, swissmap,
	// dwarf5, etc.), but those are host-specific and wrong for js/wasm:
	// register-based ABI doesn't apply, and the moxie stdlib rewrite ships
	// matched _on/_off pairs per experiment — with regabi tags set, the _on
	// versions get selected and conflict with js/wasm reality. Keep only
	// non-goexperiment entries (e.g. arch-level "amd64.v1" isn't relevant to
	// js/wasm either but costs nothing to leave in).
	filtered := make([]string, 0, len(ctx.ToolTags))
	for _, t := range ctx.ToolTags {
		if !strings.HasPrefix(t, "goexperiment.") {
			filtered = append(filtered, t)
		}
	}
	ctx.ToolTags = filtered
	return ctx
}

func mxReadDir(dir string) ([]fs.FileInfo, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	mxBases := make(map[string]bool)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".mx") {
			mxBases[strings.TrimSuffix(e.Name(), ".mx")] = true
		}
	}
	infos := make([]fs.FileInfo, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		info, err := e.Info()
		if err != nil {
			return nil, err
		}
		if strings.HasSuffix(name, ".mx") {
			infos = append(infos, renamedFileInfo{
				FileInfo: info,
				name:     strings.TrimSuffix(name, ".mx") + ".go",
			})
		} else if strings.HasSuffix(name, ".go") && mxBases[strings.TrimSuffix(name, ".go")] {
			continue
		} else {
			infos = append(infos, info)
		}
	}
	return infos, nil
}

func mxOpenFile(path string) (io.ReadCloser, error) {
	if strings.HasSuffix(path, ".go") {
		mxPath := strings.TrimSuffix(path, ".go") + ".mx"
		if f, err := os.Open(mxPath); err == nil {
			return f, nil
		}
	}
	return os.Open(path)
}

type renamedFileInfo struct {
	fs.FileInfo
	name string
}

func (r renamedFileInfo) Name() string { return r.name }

func goToMx(files []string, dir string) []string {
	out := make([]string, len(files))
	for i, f := range files {
		if strings.HasSuffix(f, ".go") {
			mxName := strings.TrimSuffix(f, ".go") + ".mx"
			if _, err := os.Stat(filepath.Join(dir, mxName)); err == nil {
				out[i] = mxName
				continue
			}
		}
		out[i] = f
	}
	return out
}

// --- Module info helpers ---

func findModFile(dir string) string {
	mxmod := filepath.Join(dir, "moxie.mod")
	if _, err := os.Stat(mxmod); err == nil {
		return mxmod
	}
	return filepath.Join(dir, "go.mod")
}

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
			return "", startDir, "", nil
		}
		dir = parent
	}
}

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
		// Format: module [version] => replacement [version]
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
		// Only handle local path replacements (starting with . or /).
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
