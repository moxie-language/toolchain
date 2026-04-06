package compiler

// This file implements Moxie language restrictions.
// Moxie removes features that are incompatible with domain-isolated
// cooperative concurrency or that obstruct the programming model.
//
// Restrictions apply to user code and converted stdlib packages.
// Runtime/internal packages are permanently exempt.

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/ssa"
)

// restrictedImports maps package paths that should not be used in Moxie code.
var restrictedImports = map[string]string{
	"strings": `use "bytes" instead of "strings" (string=[]byte)`,
}

// restrictedBuiltins maps Go builtins to the Moxie alternative.
// The loader rewrites Moxie literal syntax to (make)(...) — parenthesized
// form that the AST check skips. User-written make(...) has a bare Ident
// which this check catches.
var restrictedBuiltins = map[string]string{
	"make":    "use literal syntax: []T{:n}, chan T{}, chan T{n}",
	"new":     "use composite literal with & or var declaration",
	"complex": "complex numbers not supported in moxie",
	"real":    "complex numbers not supported in moxie",
	"imag":    "complex numbers not supported in moxie",
}

// restrictedTypes maps type names to rejection reasons.
// Note: string is NOT restricted — with string=[]byte type unification,
// the types are interchangeable. The + operator restriction on strings
// still enforces Moxie's | concatenation syntax.
var restrictedTypes = map[string]string{
	"complex64":  "complex numbers not supported in moxie",
	"complex128": "complex numbers not supported in moxie",
	"uintptr":    "use explicit pointer types",
}

// isUserPackage returns true if a package should be subject to Moxie
// language restrictions. All permanently exempt packages implement
// low-level primitives or syscall interfaces.
func isUserPackage(pkg *ssa.Package) bool {
	if pkg == nil {
		return false
	}
	path := pkg.Pkg.Path()
	if strings.HasPrefix(path, "internal/") || // internal packages
		strings.Contains(path, "/internal/") || // nested internals
		strings.HasPrefix(path, "runtime") || // language primitives
		strings.HasPrefix(path, "unsafe") || // language primitive
		strings.HasPrefix(path, "os") || // FDs, syscall wrappers
		strings.HasPrefix(path, "syscall") { // syscall interfaces
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
	// Check for restricted imports (only on the init function to avoid per-function spam).
	if b.fn.Name() == "init" || (b.fn.Name() == "main" && b.fn.Pkg.Pkg.Name() == "main") {
		for _, imp := range b.fn.Pkg.Pkg.Imports() {
			if reason, restricted := restrictedImports[imp.Path()]; restricted {
				b.addError(b.fn.Pos(), "moxie: import \""+imp.Path()+"\" is not allowed: "+reason)
			}
		}
	}
	for _, block := range b.fn.Blocks {
		for _, instr := range block.Instrs {
			b.checkInstrRestrictions(instr)
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
	// Check that every channel has both a sender and a listener.
	b.checkChannelCompleteness()
}

func (b *builder) checkInstrRestrictions(instr ssa.Instruction) {
	switch v := instr.(type) {
	case *ssa.Call:
		if builtin, ok := v.Call.Value.(*ssa.Builtin); ok {
			if reason, restricted := restrictedBuiltins[builtin.Name()]; restricted {
				b.addError(v.Pos(), "moxie: '"+builtin.Name()+"' is not allowed: "+reason)
			}
		}

	// MakeChan, MakeMap, MakeSlice are NOT checked at SSA level — the loader
	// rewrites Moxie literal syntax to (make)() calls. User-written make() is
	// caught at AST level (restrictedBuiltins) where the bare Ident is visible.

	case *ssa.BinOp:
		// Reject + on non-numeric types (strings use | for concatenation).
		if v.Op == token.ADD {
			if basic, ok := v.X.Type().Underlying().(*types.Basic); ok {
				if basic.Info()&types.IsString != 0 {
					b.addError(v.Pos(), "moxie: '+' is not allowed for text concatenation: use | operator")
				}
			}
		}

	// Note: new(T) and make() are caught by AST check (restrictedBuiltins),
		// not SSA. Restricted types (uintptr, complex) are also AST-checked.
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
	for i := 0; i < sig.Params().Len(); i++ {
		b.checkTypeAtPos(sig.Params().At(i).Type(), fn.Pos())
	}
	if sig.Results() != nil {
		for i := 0; i < sig.Results().Len(); i++ {
			b.checkTypeAtPos(sig.Results().At(i).Type(), fn.Pos())
		}
	}
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
	ast.Inspect(syntax, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.Ident:
			if reason, restricted := restrictedTypes[node.Name]; restricted {
				// uintptr is allowed in packages that import unsafe.
				if node.Name == "uintptr" && packageImportsUnsafe(b.fn.Pkg) {
					return true
				}
				b.addError(node.Pos(), "moxie: type '"+node.Name+"' is not allowed: "+reason)
			}
		case *ast.CallExpr:
			if ident, ok := node.Fun.(*ast.Ident); ok {
				if reason, restricted := restrictedBuiltins[ident.Name]; restricted {
					// Bare make() — exempt .go files and stdlib .mx overlays.
					// Loader-generated (make)() has ParenExpr as Fun, not bare Ident.
					if ident.Name == "make" {
						if isMakeExempt(b.fn.Pkg, b.program.Fset, ident.Pos()) {
							return true
						}
					}
					b.addError(ident.Pos(), "moxie: '"+ident.Name+"' is not allowed: "+reason)
				}
			}
		}
		return true
	})
}

// isMakeExempt returns true if a make() call at the given position should
// be allowed. Exempt: .go files (stdlib), stdlib .mx overlays (package
// import path has no dots), and vendored stdlib dependencies (golang.org/x/).
func isMakeExempt(pkg *ssa.Package, fset *token.FileSet, pos token.Pos) bool {
	position := fset.Position(pos)
	if !strings.HasSuffix(position.Filename, ".mx") {
		return true // .go files always allowed
	}
	// Stdlib packages have simple paths (no dots): bytes, fmt, net/http.
	// The main package also has a simple path ("main") but IS user code.
	path := pkg.Pkg.Path()
	if path == "main" || path == "command-line-arguments" {
		return false // user code
	}
	if !strings.Contains(path, ".") {
		return true // stdlib
	}
	// Vendored stdlib dependencies.
	if strings.HasPrefix(path, "golang.org/x/") {
		return true
	}
	return false
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
			// Detect spawn builtin calls.
			isSpawn := false
			var spawnArgs []ssa.Value
			if builtin, ok := call.Call.Value.(*ssa.Builtin); ok && builtin.Name() == "spawn" {
				// Builtin spawn: args are direct in call.Call.Args.
				// Skip the function arg (index 0, or 1 if transport string).
				args := call.Call.Args
				start := 0
				if len(args) > 0 {
					if _, ok := args[0].(*ssa.Const); ok {
						// Could be transport string — skip it + fn.
						start = 2
					} else {
						// fn is args[0], data starts at 1.
						start = 1
					}
				}
				if start < len(args) {
					spawnArgs = args[start:]
				}
				isSpawn = true
			}
			if !isSpawn {
				continue
			}

			for _, arg := range spawnArgs {
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

// checkChannelCompleteness ensures every channel created in a function has
// both a sender and a listener (select case or receive). Channels that escape
// the function (passed to calls, stored, returned) are exempt — the other end
// is elsewhere.
func (b *builder) checkChannelCompleteness() {
	for _, block := range b.fn.Blocks {
		for _, instr := range block.Instrs {
			mc, ok := instr.(*ssa.MakeChan)
			if !ok {
				continue
			}
			refs := mc.Referrers()
			if refs == nil || len(*refs) == 0 {
				b.addError(mc.Pos(), "moxie: channel created but never used")
				continue
			}

			hasSend := false
			hasRecv := false
			escapes := false

			for _, ref := range *refs {
				switch r := ref.(type) {
				case *ssa.Send:
					if r.Chan == mc {
						hasSend = true
					} else {
						escapes = true // channel sent as a value
					}
				case *ssa.Select:
					found := false
					for _, state := range r.States {
						if state.Chan == mc {
							found = true
							if state.Dir == types.SendOnly {
								hasSend = true
							} else {
								hasRecv = true
							}
						}
					}
					if !found {
						escapes = true // mc used as send value in select
					}
				case *ssa.UnOp:
					if r.Op == token.ARROW {
						hasRecv = true
					}
				case *ssa.Call:
					if _, ok := r.Call.Value.(*ssa.Builtin); !ok {
						escapes = true
					}
					// close, len, cap are fine — not an escape
				case *ssa.DebugRef:
					// Debug info, not real usage.
				default:
					escapes = true
				}
			}

			if escapes {
				continue
			}
			if !hasSend && !hasRecv {
				b.addError(mc.Pos(), "moxie: channel created but has no send or select/receive")
			} else if !hasSend {
				b.addError(mc.Pos(), "moxie: channel has no sender — every channel needs both a sender and a listener")
			} else if !hasRecv {
				b.addError(mc.Pos(), "moxie: channel has no listener — every channel needs both a sender and a select/receive")
			}
		}
	}
}

