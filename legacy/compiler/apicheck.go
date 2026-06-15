package compiler

// API stability enforcement for Moxie's export model.
//
// All identifiers are accessible cross-package. Capital/# prefix marks
// a symbol as part of the stable API contract. Removing or changing the
// signature of a stable symbol without bumping the major version in
// moxie.mod is a compile error when building against a cached .mxh.

import (
	"go/token"
	"go/types"
	"os"
	"strconv"
	"strings"

	"moxie/loader"
)

// checkAPIStability compares the current package's stable symbols against
// a previously cached .mxh. Called from CompilePackage after SSA build.
func (c *compilerContext) checkAPIStability(pkg *loader.Package) {
	if !isUserPackageByPath(pkg.Pkg.Path()) {
		return
	}
	prevHash := loader.MXHHashForImport(pkg.Pkg.Path())
	if prevHash == "" {
		return // no cached .mxh, nothing to compare
	}

	prevPkg, err := loader.SynthMXHPackageForCheck(pkg.Pkg.Path())
	if err != nil {
		return // can't load previous .mxh, skip check
	}

	// Collect stable symbols from current package.
	currentStable := collectStableSymbols(pkg.Pkg)
	// Collect stable symbols from cached .mxh package.
	prevStable := collectStableSymbols(prevPkg)

	// Find removals and signature changes.
	var breaks []string
	for name, prevObj := range prevStable {
		curObj, exists := currentStable[name]
		if !exists {
			breaks = append(breaks, "removed: "+name)
			continue
		}
		// Check signature compatibility for functions.
		prevFn, prevIsFn := prevObj.(*types.Func)
		curFn, curIsFn := curObj.(*types.Func)
		if prevIsFn && curIsFn {
			prevSig := prevFn.Type().(*types.Signature)
			curSig := curFn.Type().(*types.Signature)
			if !signaturesCompatible(prevSig, curSig) {
				breaks = append(breaks, "signature changed: "+name)
			}
		}
	}

	if len(breaks) == 0 {
		return
	}

	// Check if major version was bumped.
	prevVersion := loader.MXHVersionForImport(pkg.Pkg.Path())
	wd, _ := os.Getwd()
	curVersion := loader.ParseMoxieModVersion(wd)
	if majorVersionBumped(prevVersion, curVersion) {
		return // major version bumped, API breaks are allowed
	}

	for _, b := range breaks {
		c.addError(token.NoPos, "moxie: API stability violation: "+b+" (bump major version in moxie.mod)")
	}
}

// collectStableSymbols returns a map of all stable (exported) symbols
// in a package's scope. Stable means Capital letter or # prefix.
func collectStableSymbols(pkg *types.Package) map[string]types.Object {
	result := map[string]types.Object{}
	scope := pkg.Scope()
	for _, name := range scope.Names() {
		obj := scope.Lookup(name)
		if obj == nil {
			continue
		}
		if token.IsExported(name) || strings.HasPrefix(name, "#") {
			result[name] = obj
		}
	}
	return result
}

// majorVersionBumped returns true if curVersion has a higher major version
// than prevVersion. Versions are semver format "major.minor.patch".
// Returns false if either version is empty or unparseable.
func majorVersionBumped(prev, cur string) bool {
	if prev == "" || cur == "" {
		return false
	}
	prevMajor := parseMajorVersion(prev)
	curMajor := parseMajorVersion(cur)
	return curMajor > prevMajor
}

func parseMajorVersion(v string) int {
	parts := strings.SplitN(v, ".", 2)
	n, err := strconv.Atoi(parts[0])
	if err != nil {
		return -1
	}
	return n
}

// signaturesCompatible returns true if two function signatures are
// structurally identical (same parameter and return types).
func signaturesCompatible(a, b *types.Signature) bool {
	if a.Params().Len() != b.Params().Len() {
		return false
	}
	if a.Results().Len() != b.Results().Len() {
		return false
	}
	for i := 0; i < a.Params().Len(); i++ {
		if !types.Identical(a.Params().At(i).Type(), b.Params().At(i).Type()) {
			return false
		}
	}
	for i := 0; i < a.Results().Len(); i++ {
		if !types.Identical(a.Results().At(i).Type(), b.Results().At(i).Type()) {
			return false
		}
	}
	return true
}
