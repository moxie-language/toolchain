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

	"moxie/loader"

	"golang.org/x/tools/go/ssa"
)

// isUserPackage returns true if a package should be subject to Moxie
// language restrictions. Stdlib and stdlib-vendored packages are exempt.
// User packages (identified by dots in the import path from the module
// path) get full restrictions. "internal/" has no special semantics.
// The discriminator for main packages is the main package's OWN module
// path: only when the build root IS the moxie compiler module is the main
// package exempt. Merely importing mxutil (which the runtime does) does not
// exempt a program.
func (b *builder) isUserPackage(pkg *ssa.Package) bool {
	if pkg == nil {
		return false
	}
	path := pkg.Pkg.Path()
	// The moxie compiler's own packages are exempt from user restrictions.
	// The main package is the compiler when it DIRECTLY imports the moxie
	// compiler infrastructure (git.smesh.lol/moxie/pkg/...). Test programs
	// staged inside the moxie tree import only stdlib/runtime, so they stay
	// subject to the rules even though their module path is the moxie module.
	if b.pkgModulePath == "git.smesh.lol/moxie" {
		importsMoxiePkg := false
		for _, imp := range pkg.Pkg.Imports() {
			if strings.HasPrefix(imp.Path(), "git.smesh.lol/moxie/pkg/") {
				importsMoxiePkg = true
				break
			}
		}
		if importsMoxiePkg {
			return false
		}
	}
	if path == "main" || path == "command-line-arguments" {
		return true
	}
	if strings.HasPrefix(path, "golang.org/x/") {
		return false
	}
	// Compiler infrastructure packages (pkg/types, pkg/syntax, pkg/mxutil, etc.)
	// are exempt from user restrictions.
	if strings.Contains(path, "/pkg/") {
		return false
	}
	return strings.Contains(path, ".")
}

// isInitFunc returns true if fn is an init function (package init, synthetic
// init, or the init$N variants created by the Go SSA builder).
func isInitFunc(fn *ssa.Function) bool {
	name := fn.Name()
	if name == "init" {
		return true
	}
	// Init-like functions: init$guard, init$N, initFoo, etc.
	// Functions prefixed with "init" are treated as initialization
	// code that may set up package globals.
	if len(name) > 4 && name[:4] == "init" {
		return true
	}
	// Package-level var initializer (synthetic).
	if fn.Synthetic != "" && fn.Name() == "init" {
		return true
	}
	return false
}

// checkGlobalStore rejects stores that mutate package-level globals in user
// code. Globals are initialized by init() and immutable afterward.
// Sovereign self-mutating types are mutated through their methods (which
// borrow the receiver's arena); direct stores bypass that and are illegal
// for every global, sovereign or not.
func (b *builder) checkGlobalStore(addr ssa.Value, pos token.Pos) {
	if !b.isUserPackage(b.fn.Pkg) || isInitFunc(b.fn) {
		return
	}
	if gn := globalName(addr); gn != "" {
		b.addError(pos, "moxie: package globals are immutable outside init(): mutate through the methods of a sovereign self-mutating type")
	}
}

// isGlobalStore returns true if addr is a direct global variable.
// Only flags `global = value`, not `global.field = value` or
// `(*global).field = value`. Field mutations through a global pointer
// are fine - the pointer itself is immutable after init(), and the
// pointed-to struct is heap-local state.
func isGlobalStore(addr ssa.Value) bool {
	_, isGlobal := addr.(*ssa.Global)
	return isGlobal
}

// globalName extracts the global variable name from an address, if any.
// Field/index/deref stores go THROUGH the root: a global whose type is a
// pointer is not the storage itself - mutations through it land in the
// pointed-to object, which is heap-local state. Whole-global stores
// (addr is the global itself) are always reported.
func globalName(addr ssa.Value) string {
	switch v := addr.(type) {
	case *ssa.Global:
		return v.Name()
	case *ssa.FieldAddr:
		return globalNameThrough(v.X)
	case *ssa.IndexAddr:
		return globalNameThrough(v.X)
	case *ssa.UnOp:
		if v.Op == token.MUL {
			return globalNameThrough(v.X)
		}
	}
	return ""
}

// globalNameThrough reports the global an access path goes through, or ""
// if the path goes through a global pointer (mutations land in the
// pointed-to object) or is local. go/ssa wraps every Global's Type() in a
// pointer, so unwrap the Elem to recover the declared type before testing
// whether the declared type is itself a pointer.
func globalNameThrough(v ssa.Value) string {
	if g, ok := v.(*ssa.Global); ok {
		decl := g.Type()
		if p, isPtr := decl.Underlying().(*types.Pointer); isPtr {
			decl = p.Elem()
		}
		if _, isPtr := decl.Underlying().(*types.Pointer); isPtr {
			return ""
		}
		return g.Name()
	}
	return globalName(v)
}

// checkNamedReturns enforces named return values on ALL packages, including
// the stdlib. It is intentionally NOT gated by isUserPackage: the stdlib was
// converted to named returns, so the rule holds everywhere.
func (b *builder) checkNamedReturns() {
	syntax := b.fn.Syntax()
	if syntax == nil {
		return
	}
	decl, ok := syntax.(*ast.FuncDecl)
	if !ok || decl.Type.Results == nil {
		return
	}
	for _, field := range decl.Type.Results.List {
		if len(field.Names) == 0 {
			b.addError(field.Pos(), "moxie: unnamed return values are not allowed: name all return values")
		}
	}
}

// checkMoxieRestrictions scans a function for uses of restricted features.
// Model rules (channel completeness) apply to all code, stdlib included.
// The remaining checks apply only to user code — runtime packages are exempt.
func (b *builder) checkMoxieRestrictions() {
	// Every channel must have both a sender and a listener — a domain-isolation
	// invariant that holds for stdlib code too. No exemptions.
	b.checkChannelCompleteness()

	if !b.isUserPackage(b.fn.Pkg) {
		return
	}
	for _, block := range b.fn.Blocks {
		for _, instr := range block.Instrs {
			b.checkInstrRestrictions(instr)
		}
	}
	// Check for restricted types and builtins in AST (SSA may optimize them away).
	b.checkASTRestrictions()
	// Check for fallthrough (lowered away by SSA, must check AST).
	b.checkFallthroughRestriction()
	// Check that non-constant spawn args aren't used after the spawn call.
	b.checkSpawnMoveRestriction()
}

func (b *builder) checkInstrRestrictions(instr ssa.Instruction) {
	switch v := instr.(type) {
	case *ssa.BinOp:
		// Reject + on non-numeric types (strings use | for concatenation).
		if v.Op == token.ADD {
			if basic, ok := v.X.Type().Underlying().(*types.Basic); ok {
				if basic.Info()&types.IsString != 0 {
					b.addError(v.Pos(), "moxie: '+' is not allowed for text concatenation: use | operator")
				}
			}
		}

	case *ssa.Store:
		b.checkGlobalStore(v.Addr, v.Pos())

	case *ssa.MapUpdate:
		b.checkGlobalStore(v.Map, v.Pos())

	case *ssa.MakeClosure:
		// Moxie closures cannot capture variables from enclosing scopes.
		// All inputs must be explicit parameters. Package-level globals
		// are accessed directly (not via FreeVars), so they're fine.
		if len(v.Bindings) > 0 {
			fn := v.Fn.(*ssa.Function)
			var names []string
			for _, fv := range fn.FreeVars {
				names = append(names, fv.Name())
			}
			b.addError(v.Pos(), "moxie: closure captures outer variable(s) ["+strings.Join(names, ", ")+"]; pass as explicit parameter(s)")
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
			// Step 4: reject bare "any" keyword (alias for interface{}).
			if node.Name == "any" {
				b.addError(node.Pos(), "moxie: 'any' (empty interface) is not allowed: use a concrete type or named interface")
			}
		case *ast.GoStmt:
			// Step 3: reject go keyword at AST level (redundant with SSA check, fires earlier).
			b.addError(node.Pos(), "moxie: 'go' keyword is not allowed: use channels+select or spawn")
		case *ast.InterfaceType:
			// Step 4: reject empty interface literals.
			if node.Methods == nil || len(node.Methods.List) == 0 {
				b.addError(node.Pos(), "moxie: empty interface (interface{}) is not allowed: use a concrete type or named interface")
			}
		case *ast.CallExpr:
			// Ban parenthesized call targets: (f)(args) is not valid Moxie.
			// Exempt: type conversions like (*byte)(expr) where the paren wraps a type.
			if paren, ok := node.Fun.(*ast.ParenExpr); ok {
				if !isTypeExpr(paren.X) {
					b.addError(paren.Pos(), "moxie: parenthesized call target is not allowed: write f(x), not (f)(x)")
				}
			}
		case *ast.FuncDecl:
			// Exempt compiler-generated builtins (__moxie_*).
			if node.Name != nil && strings.HasPrefix(node.Name.Name, "__moxie_") {
				return true
			}
			// Step 6: reject value receivers (must use pointer receiver).
			if node.Recv != nil && len(node.Recv.List) > 0 {
				recvType := node.Recv.List[0].Type
				if _, isStar := recvType.(*ast.StarExpr); !isStar {
					b.addError(node.Recv.Pos(), "moxie: value receiver is not allowed: use pointer receiver (*T)")
				}
			}
			// Step 7: reject unnamed return values.
			if node.Type.Results != nil {
				for _, field := range node.Type.Results.List {
					if len(field.Names) == 0 {
						b.addError(field.Pos(), "moxie: unnamed return values are not allowed: name all return values")
					}
				}
			}
		}
		return true
	})
}

// isTypeExpr returns true if the AST expression is syntactically a type
// (pointer type, selector, array type, etc.) rather than a value expression.
func isTypeExpr(expr ast.Expr) bool {
	switch expr.(type) {
	case *ast.StarExpr:
		return true
	case *ast.ArrayType:
		return true
	case *ast.MapType:
		return true
	case *ast.ChanType:
		return true
	case *ast.FuncType:
		return true
	case *ast.StructType:
		return true
	case *ast.InterfaceType:
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

			// Collect every block reachable from spawn's block via successor
			// edges. A referrer in any such block is "after" spawn in the
			// control-flow sense, even if it's in a different basic block
			// (branch, loop body, later sequential block). Same-block refs
			// are handled separately by instruction-index comparison.
			reachableAfter := make(map[*ssa.BasicBlock]bool)
			var work []*ssa.BasicBlock
			for _, succ := range block.Succs {
				if !reachableAfter[succ] {
					reachableAfter[succ] = true
					work = append(work, succ)
				}
			}
			for wi := 0; wi < len(work); wi++ {
				bb := work[wi]
				for _, succ := range bb.Succs {
					if !reachableAfter[succ] {
						reachableAfter[succ] = true
						work = append(work, succ)
					}
				}
			}
			work = work[:0]

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
					refBlock := ri.Block()
					if refBlock == block {
						// Same block: referrer must come after spawn.
						// If the block has a back-edge to itself
						// (reachableAfter[block] == true), any referrer
						// in this block is reachable-after on the next
						// iteration — flag it regardless of instruction
						// position.
						if reachableAfter[block] {
							b.addError(ri.Pos(), "moxie: variable used after spawn — ownership moved to child domain")
							break
						}
						for j := spawnIdx + 1; j < len(block.Instrs); j++ {
							if block.Instrs[j] == ri {
								b.addError(ri.Pos(), "moxie: variable used after spawn — ownership moved to child domain")
								break
							}
						}
					} else if reachableAfter[refBlock] {
						b.addError(ri.Pos(), "moxie: variable used after spawn — ownership moved to child domain")
						break
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
					if bi, ok := r.Call.Value.(*ssa.Builtin); ok {
						if bi.Name() == "spawn" {
							// A channel passed to spawn escapes to the spawned
							// domain, which provides the other end.
							for _, arg := range r.Call.Args {
								if arg == mc {
									escapes = true
									break
								}
							}
						}
						// close, len, cap are fine — not an escape
					} else {
						escapes = true
					}
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

// isUserPackageByPath returns true if the import path looks like a user
// package (contains a dot, or is "main"/"command-line-arguments").
func isUserPackageByPath(path string) bool {
	if path == "main" || path == "command-line-arguments" {
		// Main packages from the moxie compiler itself (cmd/moxie, _mxc_stage4)
		// are exempt. Detect by checking if this is a directory under MOXIEROOT.
		// Note: this only works when path is "main" - the real path info
		// is provided by the caller via pkg.ImportPath for compiler packages.
		return true
	}
	if strings.HasPrefix(path, "golang.org/x/") {
		return false
	}
	return strings.Contains(path, ".")
}

// checkPackageRestrictions runs package-scope checks that aren't per-function:
// - reject var X = expr at package scope (step 8)
// - reject named slice/map types (step 9)
// - reject scope shadowing including builtins (step 10)
// - enforce init() rules: main pkg can't have init, library max one, no select/spawn
func (c *compilerContext) checkPackageRestrictions(pkg *loader.Package) {
	// Use the original ImportPath from JSON (which preserves the real module path
	// or absolute directory), not the type checker's path (which is "main" for
	// main packages).
	checkPath := pkg.ImportPath
	if checkPath == "" {
		checkPath = pkg.Pkg.Path()
	}
	// Compiler-internal packages are exempt from user restrictions.
	if strings.Contains(checkPath, "_mxc_stage4") || strings.Contains(checkPath, "cmd/moxie") {
		return
	}
	if !isUserPackageByPath(checkPath) {
		return
	}
	isMainPkg := pkg.Pkg.Name() == "main"
	initCount := 0
	for _, f := range pkg.Files {
		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Name.Name == "init" && d.Recv == nil {
					initCount++
					if isMainPkg {
						c.addError(d.Pos(), "moxie: init() is not allowed in main package: use main() for program entry")
					}
					if initCount > 1 {
						c.addError(d.Pos(), "moxie: at most one init() per package")
					}
					// Body restrictions: no select or spawn.
					if d.Body != nil {
						checkInitBodyRestrictions(c, d.Body)
					}
				}
			case *ast.GenDecl:
				switch d.Tok {
				case token.VAR:
					for _, spec := range d.Specs {
						vs := spec.(*ast.ValueSpec)
						if len(vs.Values) > 0 && !isDemotedStringConst(vs) {
							c.addError(vs.Pos(), "moxie: package-level var with initializer is not allowed: use zero-value declaration and init()")
						}
					}
				case token.TYPE:
					for _, spec := range d.Specs {
						ts := spec.(*ast.TypeSpec)
						switch ts.Type.(type) {
						case *ast.ArrayType:
							at := ts.Type.(*ast.ArrayType)
							if at.Len == nil {
								c.addError(ts.Pos(), "moxie: named slice type '"+ts.Name.Name+"' is not allowed: use []T directly")
							}
						case *ast.MapType:
							c.addError(ts.Pos(), "moxie: named map type '"+ts.Name.Name+"' is not allowed: use map[K]V directly")
						}
					}
				}
			}
		}
	}
	// Step 10: reject scope shadowing (including builtins from Universe).
	c.checkScopeShadowing(pkg)
}

// checkScopeShadowing walks all scopes in the package and rejects any
// declaration that shadows a name in an enclosing scope, including Universe.
func (c *compilerContext) checkScopeShadowing(pkg *loader.Package) {
	info := pkg.TypeInfo()
	if info == nil || info.Scopes == nil {
		return
	}
	pkgScope := pkg.Pkg.Scope()
	for _, scope := range info.Scopes {
		if scope == nil || scope == pkgScope {
			continue
		}
		for _, name := range scope.Names() {
			if name == "_" {
				continue
			}
			obj := scope.Lookup(name)
			if obj == nil {
				continue
			}
			// Walk parent scopes up to (and including) Universe.
			for parent := scope.Parent(); parent != nil; parent = parent.Parent() {
				if shadowedObj := parent.Lookup(name); shadowedObj != nil {
					c.addError(obj.Pos(), "moxie: '"+name+"' shadows declaration in enclosing scope")
					break
				}
			}
		}
	}
}

// checkInitBodyRestrictions walks an init() function body for banned constructs:
// select statements and spawn() calls.
// isDemotedStringConst returns true if a var spec was originally a string
// constant that was demoted to var by splitConstBlocks (string->[]byte).
// These have initializers like []byte("literal") and should not be flagged
// as "package-level var with initializer".
func isDemotedStringConst(vs *ast.ValueSpec) bool {
	for _, val := range vs.Values {
		call, ok := val.(*ast.CallExpr)
		if !ok {
			return false
		}
		// Check for []byte(...) call
		if _, ok := call.Fun.(*ast.ArrayType); !ok {
			return false
		}
		if len(call.Args) != 1 {
			return false
		}
		if lit, ok := call.Args[0].(*ast.BasicLit); !ok || lit.Kind != token.STRING {
			return false
		}
	}
	return true
}

func checkInitBodyRestrictions(c *compilerContext, body *ast.BlockStmt) {
	ast.Inspect(body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.SelectStmt:
			c.addError(node.Pos(), "moxie: select is not allowed in init(): event dispatch begins in main()")
		case *ast.CallExpr:
			if ident, ok := node.Fun.(*ast.Ident); ok && ident.Name == "spawn" {
				c.addError(ident.Pos(), "moxie: spawn is not allowed in init(): domains can only be created after main() starts")
			}
		}
		return true
	})
}

