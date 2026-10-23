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

	"moxie/cache"
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

	// Modules are gone: imports resolve through the layout rules (the repository
	// a package sits in, its vendor/ directory, and the installed-source cache)
	// and version pins live in MANIFEST, which the build reads. A moxie.mod left
	// in a sibling repo no longer influences resolution at all.
	r := &resolver{
		ctx:          ctx,
		modPath:      modPath,
		modDir:       modDir,
		modGoVersion: modGoVersion,
		goroot:       goroot,
		seen:         make(map[string]*PackageJSON),
		buildDir:     wd,
		moxiePath:    layoutMoxiePath(),
		moxieRoot:    goenv.Get("MOXIEROOT"),
		pins:         readManifestPins(wd, goenv.Get("MOXIEROOT")),
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
	// The input package is the program this build compiles, whether or not a
	// module file names it: the '+' text rule is gated on Module.Main, and a
	// directory target outside every module is still user code. Dependencies
	// walked before it are not the input package and keep '+'.
	if len(r.sorted) > 0 {
		last := r.sorted[len(r.sorted)-1]
		if !last.Module.Main && last.Module.Path != "git.smesh.lol/moxie" {
			last.Module.Main = true
		}
	}

	// The main package's module path is its OWN module (walking up from its
	// directory), not the cwd's module. The restriction checker uses it to
	// exempt the moxie compiler's own main package while keeping every
	// other program subject to the rules.
	if mp := r.seen[inputPkg]; mp != nil && mp.Module.Path == "" {
		ownDir := mp.Dir
		if ownDir == "" {
			ownDir = wd
		}
		ownMod, ownModDir, _, modErr := readModInfo(ownDir)
		if modErr == nil {
			// The input package is this build's program even when no module
			// file names it: the '+' text rule is gated on Module.Main, and a
			// directory target outside every module is still user code. A
			// dependency package is not the input package and keeps '+'.
			mp.Module.Path = ownMod
			mp.Module.Main = true
			mp.Module.Dir = ownModDir
		}
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
	copy(r.sorted[targetIdx:], r.sorted[targetIdx+1:])
	r.sorted = r.sorted[:len(r.sorted)-1]
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
	goroot       string
	seen         map[string]*PackageJSON
	sorted       []*PackageJSON
	// The layout rules stage4 shares for trees with no module file: a
	// repository is its checkout under MOXIEPATH, or the worktree its MANIFEST
	// pin names, and the toolchain repository is MOXIEROOT.
	buildDir  string
	moxiePath string
	moxieRoot string
	pins      map[string]string // repository path → pinned ref
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
		return fmt.Errorf("resolving %q: %w\n\thint: run `moxie install %s` to cache this package", importPath, err, importPath)
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
	// Packages get a synthesized String() method whose body calls
	// runtime.RenderValue (loader/stringers.go), so the dependency graph has
	// to carry the runtime edge the source does not spell out. Packages inside
	// runtime's own import closure are skipped: the edge would be a cycle.
	// Adding it for every other package only over-approximates the graph; a
	// package whose types need no synthesized method simply does not import
	// runtime in its source.
	hasRuntime := false
	for _, imp := range allImports {
		if imp == "runtime" {
			hasRuntime = true
		}
	}
	if !hasRuntime && importPath != "runtime" && !renderSkipPkg(importPath) &&
		renderUserPkg(dir, r.goroot) {
		allImports = append(allImports, "runtime")
	}
	for _, imp := range allImports {
		if false {
			fmt.Fprintf(os.Stderr, "DEBUG: %q imports %q (dir=%s)\n", importPath, imp, dir)
		}
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
		Imports:    allImports,
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

	// 1b. Absolute paths and bare directory names (e.g. _mxc_stage4/).
	if filepath.IsAbs(importPath) {
		if isDir(importPath) {
			return importPath, nil
		}
		return "", fmt.Errorf("package directory %q not found", importPath)
	}
	// Bare directory in working directory (e.g. "_mxc_stage4/").
	candidate := filepath.Join(srcDir, importPath)
	if isDir(candidate) {
		return candidate, nil
	}
	candidate = filepath.Join(srcDir, importPath+"/")
	if isDir(candidate) {
		return candidate, nil
	}

	// 2. Standard library (in synthetic GOROOT). Its paths are reserved: a
	// module-relative lookup must not shadow them. The smesh tree nests a
	// web/common module that contains crypto/hkdf, and resolving the stdlib
	// crypto/hkdf to that directory made the package import itself.
	stdDir := filepath.Join(r.goroot, "src", importPath)
	if isDir(stdDir) {
		return stdDir, nil
	}
	// 2b. Stdlib-vendored packages (e.g. golang.org/x/crypto).
	stdVendorDir := filepath.Join(r.goroot, "src", "vendor", importPath)
	if isDir(stdVendorDir) {
		return stdVendorDir, nil
	}

	// 2c. Module-relative path (without module prefix).
	if r.modDir != "" {
		candidate = filepath.Join(r.modDir, importPath)
		if isDir(candidate) {
			return candidate, nil
		}
		candidate = filepath.Join(r.modDir, importPath+"/")
		if isDir(candidate) {
			return candidate, nil
		}
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

	// 3b. Repository layout: a sibling repository import resolves to its
	// checkout under MOXIEPATH, or to the worktree a MANIFEST pin names. This
	// is the model stage4 already uses and the one the module files are being
	// removed in favour of, so it wins over the module readers below.
	if dir, err := r.layoutRepoDir(importPath); err != nil {
		return "", err
	} else if dir != "" {
		return dir, nil
	}

	// 4. Vendored packages.
	if r.modDir != "" {
		vendorDir := filepath.Join(r.modDir, "vendor", importPath)
		if isDir(vendorDir) {
			return vendorDir, nil
		}
	}

	// 5. Installed source (mxinstall cache): `moxie install` clones the
	// source here. Installed packages compile from source like every other
	// package - no precompiled-binary exception path.
	if srcDirInstalled := cache.SourcePath(importPath); isDir(srcDirInstalled) {
		return srcDirInstalled, nil
	}

	// 6. The compiler's own infrastructure packages (git.smesh.lol/moxie/pkg/
	// -> MOXIEROOT/pkg/). The runtime and converted stdlib import these
	// (e.g. mxutil), so they must resolve for any module, not just the moxie
	// module itself.
	if strings.HasPrefix(importPath, "git.smesh.lol/moxie/pkg/") {
		if root := goenv.Get("MOXIEROOT"); root != "" {
			rel := strings.TrimPrefix(importPath, "git.smesh.lol/moxie/")
			dir := filepath.Join(root, rel)
			if isDir(dir) {
				return dir, nil
			}
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

// readModInfo returns the module path and directory a package belongs to.
//
// There is no module file to read: Moxie removed modules, so the repository the
// directory sits in IS the module - the toolchain tree inside MOXIEROOT, a
// checkout under MOXIEPATH (its first two path components), and nothing at all
// outside both, which leaves the package subject to the language restrictions as
// user code should be. A `moxie.mod` or `go.mod` still sitting in a sibling repo
// is ignored rather than preferred; version pins live in MANIFEST, which the
// build already reads. The goVersion is therefore always empty and the checker
// uses its default language version.
func readModInfo(startDir string) (modPath, modDir, goVersion string, err error) {
	if mp, md := moduleFromRoots(startDir); mp != "" {
		return mp, md, "", nil
	}
	return "", startDir, "", nil
}

// moduleFromRoots derives a module path and directory from the layout rules
// the stage4 compiler shares, for trees that carry no module file: MOXIEROOT
// is the toolchain module, and a directory under MOXIEPATH belongs to the
// repository named by its first two components. A directory outside both has
// no module.
func moduleFromRoots(dir string) (modPath, modDir string) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", ""
	}
	if root := goenv.Get("MOXIEROOT"); root != "" {
		if rabs, err2 := filepath.Abs(root); err2 == nil && withinDir(abs, rabs) {
			return "git.smesh.lol/moxie", rabs
		}
	}
	mpath := os.Getenv("MOXIEPATH")
	if mpath == "" {
		if home, err2 := os.UserHomeDir(); err2 == nil {
			mpath = filepath.Join(home, "moxie")
		}
	}
	if mpath == "" {
		return "", ""
	}
	mabs, err2 := filepath.Abs(mpath)
	if err2 != nil || !withinDir(abs, mabs) {
		return "", ""
	}
	rel, err3 := filepath.Rel(mabs, abs)
	if err3 != nil {
		return "", ""
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", ""
	}
	return parts[0] + "/" + parts[1], filepath.Join(mabs, parts[0], parts[1])
}

// withinDir reports whether dir is root or sits under it.
func withinDir(dir, root string) bool {
	if dir == root {
		return true
	}
	return strings.HasPrefix(dir, root+string(filepath.Separator))
}

// layoutMoxiePath is the directory repositories are checked out under.
// MOXIEPATH overrides it; the default matches the stage4 compiler's.
func layoutMoxiePath() string {
	if p := os.Getenv("MOXIEPATH"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "moxie")
}

// splitRepoPath cuts a repository import path into its repository (the first
// two components) and the package path inside it. ok is false for paths that
// are not laid out that way, e.g. a single-component path or a relative one.
func splitRepoPath(importPath string) (repo, rel string, ok bool) {
	first := strings.Index(importPath, "/")
	if first <= 0 {
		return "", "", false
	}
	second := strings.Index(importPath[first+1:], "/")
	if second < 0 {
		return importPath, "", true
	}
	second += first + 1
	return importPath[:second], importPath[second+1:], true
}

// readManifestPins returns the repository → ref pins in the build root's
// MANIFEST, falling back to MOXIEROOT's. The grammar is the one stage4 reads:
// single `require <repo> <ref>` lines and `require ( <repo> <ref> ... )` blocks.
func readManifestPins(buildDir, root string) map[string]string {
	var data []byte
	for _, dir := range []string{buildDir, root} {
		if dir == "" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, "MANIFEST"))
		if err == nil {
			data = b
			break
		}
	}
	if data == nil {
		return nil
	}
	pins := make(map[string]string)
	inBlock := false
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "//") || strings.HasPrefix(line, "#") {
			continue
		}
		if line == "require (" {
			inBlock = true
			continue
		}
		if inBlock && line == ")" {
			inBlock = false
			continue
		}
		if strings.HasPrefix(line, "require ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "require"))
		} else if !inBlock {
			continue
		}
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		ref := ""
		if len(f) > 1 {
			ref = f[1]
		}
		pins[f[0]] = ref
	}
	return pins
}

// layoutRepoDir resolves a repository import path through the layout rules.
// The toolchain repository is MOXIEROOT; a pinned repository is the worktree
// its pin names under MOXIEPATH/.refs, which `moxie get` materialises; every
// other repository is its checkout under MOXIEPATH. It returns "" when the
// path is not a repository import or no tree exists for it, letting the
// module readers below have their turn.
//
// A pin whose worktree is missing is an error, not a fallback to the checkout:
// silently building a version nobody asked for is the failure stage4 already
// refuses.
func (r *resolver) layoutRepoDir(importPath string) (string, error) {
	repo, rel, ok := splitRepoPath(importPath)
	if !ok {
		return "", nil
	}
	if repo == "git.smesh.lol/moxie" {
		if r.moxieRoot == "" {
			return "", nil
		}
		dir := filepath.Join(r.moxieRoot, rel)
		if isDir(dir) {
			return dir, nil
		}
		return "", nil
	}
	if r.moxiePath == "" {
		return "", nil
	}
	if ref := r.pins[repo]; ref != "" {
		wt := filepath.Join(r.moxiePath, ".refs", repo, ref)
		if !isDir(wt) {
			return "", fmt.Errorf("MANIFEST pins %s to %s but %s is not materialised; run `moxie get %s@%s`",
				repo, ref, wt, repo, ref)
		}
		dir := wt
		if rel != "" {
			dir = filepath.Join(wt, rel)
		}
		if isDir(dir) {
			return dir, nil
		}
		return "", fmt.Errorf("MANIFEST: %s %s has no package %s", repo, ref, importPath)
	}
	base := filepath.Join(r.moxiePath, repo)
	if !isDir(base) {
		return "", nil
	}
	dir := base
	if rel != "" {
		dir = filepath.Join(base, rel)
	}
	if isDir(dir) {
		return dir, nil
	}
	return "", nil
}

// ParseMoxieModVersion reads a moxie.mod file and returns the moxie version
// string (e.g. "1.2.0"). Returns empty string if not found.
func ParseMoxieModVersion(dir string) string {
	for _, name := range []string{"moxie.mod", "go.mod"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "moxie ") {
				return strings.TrimSpace(strings.TrimPrefix(line, "moxie"))
			}
		}
	}
	return ""
}
func isDir(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}
