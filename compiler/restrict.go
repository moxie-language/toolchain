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
	"strings"

	"moxie/mxssa"
	"moxie/typecheck"
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

// isUserPackageTC reports whether a package should be subject to Moxie
// language restrictions. All permanently exempt packages implement
// low-level primitives or syscall interfaces.
func isUserPackageTC(pkg *mxssa.Package) bool {
	if pkg == nil || pkg.Pkg == nil {
		return false
	}
	path := pkg.Pkg.Path()
	if strings.HasPrefix(path, "internal/") ||
		strings.Contains(path, "/internal/") ||
		strings.HasPrefix(path, "runtime") ||
		strings.HasPrefix(path, "unsafe") ||
		strings.HasPrefix(path, "os") ||
		strings.HasPrefix(path, "syscall") {
		return false
	}
	return true
}

// checkMoxieRestrictions scans a function for uses of restricted features.
// Only applies to user code — runtime packages are exempt.
//
// Returns silently when mxFn is unpopulated (external decls without bodies,
// or functions that failed mxssa translation). Restriction checks against
// such functions aren't meaningful — they have no instructions to inspect
// and AST checks would have already fired during loader/parser passes.
func (b *builder) checkMoxieRestrictions() {
	if b.mxFn == nil || len(b.mxFn.Blocks) == 0 {
		return
	}
	if !isUserPackageTC(b.mxFn.Pkg) {
		return
	}
	if b.mxFn.Name() == "init" || (b.mxFn.Name() == "main" && b.mxFn.Pkg.Pkg.Name() == "main") {
		for _, imp := range b.mxFn.Pkg.Pkg.Imports() {
			if reason, restricted := restrictedImports[imp.Path()]; restricted {
				b.addError(b.mxFn.Pos(), "moxie: import \""+imp.Path()+"\" is not allowed: "+reason)
			}
		}
	}
	for _, block := range b.mxFn.Blocks {
		for _, instr := range block.Instrs {
			b.checkInstrRestrictionsTC(instr)
		}
	}
	b.checkSignatureRestrictionsTC()
	b.checkFallthroughRestrictionTC()
	b.checkASTRestrictionsTC()
	b.checkSpawnMoveRestrictionTC()
	b.checkChannelCompletenessTC()
}

// checkInstrRestrictionsTC catches restricted builtins (make/new/complex/etc.)
// and '+' on text types.
func (b *builder) checkInstrRestrictionsTC(instr mxssa.Instruction) {
	switch v := instr.(type) {
	case *mxssa.Call:
		if builtin, ok := v.Call.Value.(*mxssa.Builtin); ok {
			if reason, restricted := restrictedBuiltins[builtin.Name()]; restricted {
				b.addError(v.Pos(), "moxie: '"+builtin.Name()+"' is not allowed: "+reason)
			}
		}
	case *mxssa.BinOp:
		if v.Op == token.ADD {
			if typecheck.HasInfo(v.X.Type(), typecheck.IsString) {
				b.addError(v.Pos(), "moxie: '+' is not allowed for text concatenation: use | operator")
			}
		}
	}
}

// checkTypeAtPosTC reports a restricted-type error for t (uintptr/complex).
func (b *builder) checkTypeAtPosTC(t typecheck.Type, pos token.Pos) {
	if t == nil {
		return
	}
	if ptr, ok := t.(*typecheck.Pointer); ok {
		t = ptr.Elem()
	}
	basic, ok := t.(*typecheck.Basic)
	if !ok {
		return
	}
	if reason, restricted := restrictedTypes[basic.Name()]; restricted {
		if basic.Name() == "uintptr" && packageImportsUnsafeTC(b.mxFn.Pkg) {
			return
		}
		b.addError(pos, "moxie: type '"+basic.Name()+"' is not allowed: "+reason)
	}
}

// checkSignatureRestrictionsTC is the mxssa variant of checkSignatureRestrictions.
func (b *builder) checkSignatureRestrictionsTC() {
	sig := b.mxFn.Signature
	pos := b.mxFn.Pos()
	if params := sig.Params(); params != nil {
		for i := 0; i < params.Len(); i++ {
			b.checkTypeAtPosTC(params.At(i).Type(), pos)
		}
	}
	if results := sig.Results(); results != nil {
		for i := 0; i < results.Len(); i++ {
			b.checkTypeAtPosTC(results.At(i).Type(), pos)
		}
	}
}

// packageImportsUnsafeTC reports whether the package imports "unsafe".
// Packages using unsafe.Pointer legitimately need uintptr for pointer arithmetic.
func packageImportsUnsafeTC(pkg *mxssa.Package) bool {
	if pkg == nil || pkg.Pkg == nil {
		return false
	}
	for _, imp := range pkg.Pkg.Imports() {
		if imp.Path() == "unsafe" {
			return true
		}
	}
	return false
}

// isMakeExemptTC reports whether a make() call at the given position should
// be allowed. Exempt: .go files (stdlib), stdlib .mx overlays (package import
// path has no dots), and vendored stdlib dependencies (golang.org/x/).
func isMakeExemptTC(pkg *mxssa.Package, fset *token.FileSet, pos token.Pos) bool {
	position := fset.Position(pos)
	if !strings.HasSuffix(position.Filename, ".mx") {
		return true
	}
	if pkg == nil || pkg.Pkg == nil {
		return false
	}
	path := pkg.Pkg.Path()
	if path == "main" || path == "command-line-arguments" {
		return false
	}
	if !strings.Contains(path, ".") {
		return true
	}
	if strings.HasPrefix(path, "golang.org/x/") {
		return true
	}
	return false
}

// checkFallthroughRestrictionTC is the mxssa variant of checkFallthroughRestriction.
func (b *builder) checkFallthroughRestrictionTC() {
	syntax := b.mxFn.Syntax()
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

// checkASTRestrictionsTC is the mxssa variant of checkASTRestrictions.
func (b *builder) checkASTRestrictionsTC() {
	syntax := b.mxFn.Syntax()
	if syntax == nil {
		return
	}
	ast.Inspect(syntax, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.Ident:
			if reason, restricted := restrictedTypes[node.Name]; restricted {
				if node.Name == "uintptr" && packageImportsUnsafeTC(b.mxFn.Pkg) {
					return true
				}
				b.addError(node.Pos(), "moxie: type '"+node.Name+"' is not allowed: "+reason)
			}
		case *ast.CallExpr:
			if ident, ok := node.Fun.(*ast.Ident); ok {
				if reason, restricted := restrictedBuiltins[ident.Name]; restricted {
					if ident.Name == "make" {
						if isMakeExemptTC(b.mxFn.Pkg, b.mxProgram.Fset, ident.Pos()) {
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

// checkSpawnMoveRestrictionTC enforces move semantics for spawn arguments.
// Non-constant values passed to spawn must not be used after the call —
// ownership moves to the child domain. No shared memory.
func (b *builder) checkSpawnMoveRestrictionTC() {
	for _, block := range b.mxFn.Blocks {
		for spawnIdx, instr := range block.Instrs {
			call, ok := instr.(*mxssa.Call)
			if !ok {
				continue
			}
			isSpawn := false
			var spawnArgs []mxssa.Value
			if builtin, ok := call.Call.Value.(*mxssa.Builtin); ok && builtin.Name() == "spawn" {
				args := call.Call.Args
				start := 0
				if len(args) > 0 {
					if _, ok := args[0].(*mxssa.Const); ok {
						start = 2
					} else {
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

			reachableAfter := make(map[*mxssa.BasicBlock]bool)
			var work []*mxssa.BasicBlock
			for _, succ := range block.Succs {
				if !reachableAfter[succ] {
					reachableAfter[succ] = true
					work = append(work, succ)
				}
			}
			for len(work) > 0 {
				bb := work[0]
				work = work[1:]
				for _, succ := range bb.Succs {
					if !reachableAfter[succ] {
						reachableAfter[succ] = true
						work = append(work, succ)
					}
				}
			}

			for _, arg := range spawnArgs {
				if _, isConst := arg.(*mxssa.Const); isConst {
					continue
				}
				if _, isChan := arg.Type().Underlying().(*typecheck.Chan); isChan {
					continue
				}
				refs := arg.Referrers()
				if refs == nil {
					continue
				}
				for _, ref := range *refs {
					refBlock := ref.Block()
					if refBlock == block {
						if reachableAfter[block] {
							b.addError(ref.Pos(), "moxie: variable used after spawn — ownership moved to child domain")
							break
						}
						for j := spawnIdx + 1; j < len(block.Instrs); j++ {
							if block.Instrs[j] == ref {
								b.addError(ref.Pos(), "moxie: variable used after spawn — ownership moved to child domain")
								break
							}
						}
					} else if reachableAfter[refBlock] {
						b.addError(ref.Pos(), "moxie: variable used after spawn — ownership moved to child domain")
						break
					}
				}
			}
		}
	}
}

// checkChannelCompletenessTC ensures every channel created in a function has
// both a sender and a listener (select case or receive). Channels that escape
// the function (passed to calls, stored, returned) are exempt — the other end
// is elsewhere.
func (b *builder) checkChannelCompletenessTC() {
	for _, block := range b.mxFn.Blocks {
		for _, instr := range block.Instrs {
			mc, ok := instr.(*mxssa.MakeChan)
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
			var mcVal mxssa.Value = mc

			for _, ref := range *refs {
				switch r := ref.(type) {
				case *mxssa.Send:
					if r.Chan == mcVal {
						hasSend = true
					} else {
						escapes = true
					}
				case *mxssa.Select:
					found := false
					for _, state := range r.States {
						if state.Chan == mcVal {
							found = true
							// ssabridge encodes send as token.CHAN, recv as token.ARROW.
							if state.Dir == token.CHAN {
								hasSend = true
							} else {
								hasRecv = true
							}
						}
					}
					if !found {
						escapes = true
					}
				case *mxssa.UnOp:
					if r.Op == token.ARROW {
						hasRecv = true
					}
				case *mxssa.Call:
					if _, ok := r.Call.Value.(*mxssa.Builtin); !ok {
						escapes = true
					}
					// close, len, cap are builtins — not an escape.
				case *mxssa.DebugRef:
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

