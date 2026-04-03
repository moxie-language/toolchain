package compiler

// This file implements Moxie language restrictions.
// Moxie removes features that are incompatible with domain-isolated
// cooperative concurrency or that obstract the programming model.
//
// Restrictions apply to user code and stdlib packages that have been
// converted to Moxie syntax (listed in loader.MoxiePurePackages).
// Runtime/internal packages are permanently exempt.

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/ssa"
)

// restrictedBuiltins maps Go builtins to the Moxie alternative.
// Note: make is NOT listed here because the compiler generates make() calls
// from Moxie syntax ([]T{:n} → make([]T, n)). Channel and map makes are
// caught at SSA level (MakeChan, MakeMap) instead.
var restrictedBuiltins = map[string]string{
	"new":     "use composite literal with & or var declaration",
	"complex": "complex numbers not supported in moxie",
	"real":    "complex numbers not supported in moxie",
	"imag":    "complex numbers not supported in moxie",
}

// restrictedTypes maps type names to rejection reasons.
var restrictedTypes = map[string]string{
	"complex64":  "complex numbers not supported in moxie",
	"complex128": "complex numbers not supported in moxie",
	"uintptr":    "use explicit pointer types",
	"string":     "use []byte",
}

// isUserPackage returns true if a package should be subject to Moxie
// language restrictions. Exempt packages fall into two categories:
//   - Permanent: runtime/*, internal/*, unsafe, reflect — implement primitives
//   - Unconverted: still use string type, new(), or complex in signatures.
//     These need mxpurify conversion before they can be enforced.
func isUserPackage(pkg *ssa.Package) bool {
	if pkg == nil {
		return false
	}
	path := pkg.Pkg.Path()
	// Permanent exemptions — low-level primitives.
	if strings.HasPrefix(path, "internal/") ||
		strings.Contains(path, "/internal/") ||
		strings.HasPrefix(path, "runtime") ||
		strings.HasPrefix(path, "unsafe") ||
		path == "reflect" {
		return false
	}
	// Unconverted packages — string/complex/new violations remain.
	switch path {
	case "encoding/binary", // complex number support
		"strconv",  // complex number support, string+ concat
		"strings",  // string type throughout
		"fmt",      // complex, uintptr, string type, string+ concat
		"math/big", // fallthrough, string+ concat
		"time",     // string type, string+ concat
		"io/fs":    // string type in function signatures
		return false
	}
	if strings.HasPrefix(path, "os") || // string, new(), string+
		strings.HasPrefix(path, "syscall") { // string, new(), string+
		return false
	}
	return true
}

// packageImportsUnsafe returns true if the package imports "unsafe".
// Packages using unsafe.Pointer legitimately need uintptr for pointer arithmetic.
func packageImportsUnsafe(pkg *ssa.Package) bool {
	for _, imp := range pkg.Pkg.Imports() {
		if imp.Path() == "unsafe" {
			return true
		}
	}
	return false
}

// checkMoxieRestrictions scans a function for uses of restricted features.
// Only applies to user code — runtime packages are exempt.
func (b *builder) checkMoxieRestrictions() {
	if !isUserPackage(b.fn.Pkg) {
		return
	}
	exempt := isStringExemptMethod(b.fn)
	if !exempt {
		for _, block := range b.fn.Blocks {
			for _, instr := range block.Instrs {
				b.checkInstrRestrictions(instr)
			}
		}
	}
	// Check function signature for restricted types.
	b.checkSignatureRestrictions(b.fn)
	// Check for fallthrough (lowered away by SSA, must check AST).
	b.checkFallthroughRestriction()
	// Check for restricted types and builtins in AST (SSA may optimize them away).
	b.checkASTRestrictions()
	// Check that non-constant spawn args aren't used after the spawn call.
	b.checkSpawnMoveRestriction()
}

func (b *builder) checkInstrRestrictions(instr ssa.Instruction) {
	switch v := instr.(type) {
	case *ssa.Call:
		if builtin, ok := v.Call.Value.(*ssa.Builtin); ok {
			if reason, restricted := restrictedBuiltins[builtin.Name()]; restricted {
				b.addError(v.Pos(), "moxie: '"+builtin.Name()+"' is not allowed: "+reason)
			}
		}

	// MakeChan and MakeMap are NOT checked at SSA level — both make(chan T)
	// and chan T{} produce MakeChan; both make(map[K]V) and map[K]V{} produce
	// MakeMap. AST-level checks in checkASTRestrictions distinguish them.
	// MakeSlice is NOT restricted — the compiler generates make() calls from
	// Moxie slice literal syntax ([]T{:n} → make([]T, n)).

	case *ssa.BinOp:
		// Reject + on non-numeric types (strings use | for concatenation).
		if v.Op == token.ADD {
			if basic, ok := v.X.Type().Underlying().(*types.Basic); ok {
				if basic.Info()&types.IsString != 0 {
					b.addError(v.Pos(), "moxie: '+' is not allowed for text concatenation: use | operator")
				}
			}
		}

	// Note: new(T) is caught by AST check (restrictedBuiltins), not SSA.
		// SSA Alloc with Comment="new" has false positives when a local
		// variable named "new" has its address taken (&new).
		// Restricted types (uintptr, complex) are also caught by AST checks.
	}
}

func (b *builder) checkTypeAtPos(t types.Type, pos token.Pos) {
	if t == nil {
		return
	}
	// Unwrap pointer types — SSA wraps local var types in pointers.
	if ptr, ok := t.(*types.Pointer); ok {
		t = ptr.Elem()
	}
	basic, ok := t.(*types.Basic)
	if !ok {
		return
	}
	if reason, restricted := restrictedTypes[basic.Name()]; restricted {
		// uintptr is allowed in packages that import unsafe — needed for
		// pointer arithmetic with unsafe.Pointer.
		if basic.Name() == "uintptr" && packageImportsUnsafe(b.fn.Pkg) {
			return
		}
		b.addError(pos, "moxie: type '"+basic.Name()+"' is not allowed: "+reason)
	}
}

func (b *builder) checkSignatureRestrictions(fn *ssa.Function) {
	sig := fn.Signature
	// String is allowed in function signatures (params and returns) for
	// cross-package boundary compatibility — the system go/types does not
	// support string↔[]byte assignability. Restriction enforcement focuses
	// on internal types (struct fields, local variables).
	for i := 0; i < sig.Params().Len(); i++ {
		t := sig.Params().At(i).Type()
		if basic, ok := t.(*types.Basic); ok && basic.Name() == "string" {
			continue
		}
		b.checkTypeAtPos(t, fn.Pos())
	}
	if sig.Results() != nil {
		for i := 0; i < sig.Results().Len(); i++ {
			t := sig.Results().At(i).Type()
			if basic, ok := t.(*types.Basic); ok && basic.Name() == "string" {
				continue
			}
			b.checkTypeAtPos(t, fn.Pos())
		}
	}
}

// isStringExemptMethod returns true for methods that must return string
// to satisfy Go built-in interfaces (error, fmt.Stringer). These methods
// cannot be converted to []byte because the interface is hardcoded in
// Go's types.Universe.
func isStringExemptMethod(fn *ssa.Function) bool {
	sig := fn.Signature
	if sig.Recv() == nil {
		return false
	}
	switch fn.Name() {
	case "Error", "String":
		return sig.Params().Len() == 0 && sig.Results().Len() == 1
	}
	return false
}

// checkFallthroughRestriction walks the function's AST looking for
// fallthrough statements. SSA eliminates these, so we must check the
// raw syntax tree.
func (b *builder) checkFallthroughRestriction() {
	syntax := b.fn.Syntax()
	if syntax == nil {
		return
	}
	ast.Inspect(syntax, func(n ast.Node) bool {
		if br, ok := n.(*ast.BranchStmt); ok && br.Tok == token.FALLTHROUGH {
			b.addError(br.Pos(), "moxie: 'fallthrough' is not allowed: each case must be self-contained")
			return false
		}
		return true
	})
}

// checkASTRestrictions walks the function's AST to catch restricted types
// and builtins that SSA may optimize away (dead code elimination removes
// unused complex64 vars, unused complex() calls, etc).
func (b *builder) checkASTRestrictions() {
	syntax := b.fn.Syntax()
	if syntax == nil {
		return
	}
	exempt := isStringExemptMethod(b.fn)
	// Collect positions of string identifiers used as type conversions
	// (e.g. string(b) for UTF-8 rune iteration). These are exempt from
	// the string type restriction.
	stringExemptPositions := map[token.Pos]bool{}
	ast.Inspect(syntax, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if ident, ok := call.Fun.(*ast.Ident); ok && ident.Name == "string" {
				stringExemptPositions[ident.Pos()] = true
			}
		}
		// Exempt string in function signatures (params + returns) and
		// variable declarations for cross-package boundary compatibility.
		// Local vars must match function return types from system go/types.
		if ft, ok := n.(*ast.FuncType); ok {
			for _, fields := range []*ast.FieldList{ft.Params, ft.Results} {
				if fields == nil {
					continue
				}
				for _, f := range fields.List {
					if id, ok := f.Type.(*ast.Ident); ok && id.Name == "string" {
						stringExemptPositions[id.Pos()] = true
					}
					// Variadic ...string: unwrap ellipsis.
					if ell, ok := f.Type.(*ast.Ellipsis); ok {
						if id, ok := ell.Elt.(*ast.Ident); ok && id.Name == "string" {
							stringExemptPositions[id.Pos()] = true
						}
					}
				}
			}
		}
		// Exempt string in var/const type annotations (var x string).
		if vs, ok := n.(*ast.ValueSpec); ok {
			if id, ok := vs.Type.(*ast.Ident); ok && id.Name == "string" {
				stringExemptPositions[id.Pos()] = true
			}
		}
		return true
	})

	ast.Inspect(syntax, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.Ident:
			// Exempt string in Error()/String() methods — these must use
			// string to satisfy Go's built-in error/Stringer interfaces.
			if node.Name == "string" && exempt {
				return true
			}
			// Exempt string() type conversions and function signature positions.
			if node.Name == "string" && stringExemptPositions[node.Pos()] {
				return true
			}
			if reason, restricted := restrictedTypes[node.Name]; restricted {
				// uintptr is allowed in packages that import unsafe.
				if node.Name == "uintptr" && packageImportsUnsafe(b.fn.Pkg) {
					return true
				}
				b.addError(node.Pos(), "moxie: type '"+node.Name+"' is not allowed: "+reason)
			}
		case *ast.CallExpr:
			if ident, ok := node.Fun.(*ast.Ident); ok {
				// Note: make(chan T) and make(map[K]V) are NOT restricted at AST
				// level because rewriteChanLiterals converts chan T{} → make(chan T)
				// before parsing. Both user-written and rewriter-generated make()
				// calls look identical at this point.
				if reason, restricted := restrictedBuiltins[ident.Name]; restricted {
					b.addError(ident.Pos(), "moxie: '"+ident.Name+"' is not allowed: "+reason)
				}
			}
		}
		return true
	})
}

// checkSpawnMoveRestriction enforces move semantics for spawn arguments.
// Non-constant values passed to spawn must not be used after the call —
// ownership moves to the child domain. No shared memory.
func (b *builder) checkSpawnMoveRestriction() {
	for _, block := range b.fn.Blocks {
		for spawnIdx, instr := range block.Instrs {
			call, ok := instr.(*ssa.Call)
			if !ok {
				continue
			}
			callee := call.Call.StaticCallee()
			if callee == nil || callee.Name() != "spawn" {
				continue
			}

			args := extractSpawnSSAArgs(&call.Call)
			exempt := spawnVarargsInstrs(&call.Call)

			for _, arg := range args {
				if _, isConst := arg.(*ssa.Const); isConst {
					continue
				}
				// Channels are exempt — both parent and child hold
				// endpoints for IPC communication.
				if _, isChan := arg.Type().Underlying().(*types.Chan); isChan {
					continue
				}
				refs := arg.Referrers()
				if refs == nil {
					continue
				}
				for _, ref := range *refs {
					ri, ok := ref.(ssa.Instruction)
					if !ok {
						continue
					}
					if exempt[ri] {
						continue
					}
					// Same block: check if the referrer comes after spawn.
					if ri.Block() == block {
						for j := spawnIdx + 1; j < len(block.Instrs); j++ {
							if block.Instrs[j] == ri {
								b.addError(ri.Pos(), "moxie: variable used after spawn — ownership moved to child domain")
								break
							}
						}
					}
				}
			}
		}
	}
}

// spawnVarargsInstrs collects all SSA instructions that are part of the
// spawn call's variadic argument construction. These are exempt from the
// move restriction since they happen before the spawn.
func spawnVarargsInstrs(instr *ssa.CallCommon) map[ssa.Instruction]bool {
	set := make(map[ssa.Instruction]bool)
	if len(instr.Args) < 2 {
		return set
	}
	slice, ok := instr.Args[1].(*ssa.Slice)
	if !ok {
		return set
	}
	set[slice] = true
	alloc, ok := slice.X.(*ssa.Alloc)
	if !ok {
		return set
	}
	set[alloc] = true
	refs := alloc.Referrers()
	if refs == nil {
		return set
	}
	for _, ref := range *refs {
		ia, ok := ref.(*ssa.IndexAddr)
		if !ok {
			continue
		}
		set[ia] = true
		iaRefs := ia.Referrers()
		if iaRefs == nil {
			continue
		}
		for _, iaRef := range *iaRefs {
			if store, ok := iaRef.(*ssa.Store); ok && store.Addr == ia {
				set[store] = true
				if mi, ok := store.Val.(*ssa.MakeInterface); ok {
					set[mi] = true
				}
			}
		}
	}
	// Also exempt the fn arg's MakeInterface.
	if mi, ok := instr.Args[0].(*ssa.MakeInterface); ok {
		set[mi] = true
	}
	return set
}
