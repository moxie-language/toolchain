package compiler

// This file implements Moxie language restrictions.
// Moxie removes features that are incompatible with domain-isolated
// cooperative concurrency or that obstruct the programming model.
//
// Restrictions apply to user code and converted stdlib packages.
// Runtime/internal packages are permanently exempt.

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"sort"
	"strconv"
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
	// A package that DIRECTLY imports the moxie compiler infrastructure
	// (git.smesh.lol/moxie/pkg/...) is compiler code. A build rooted at the
	// moxie module also compiles its own infrastructure packages directly
	// (path becomes "command-line-arguments"); those are exempt too, mirroring
	// stage4's buildHasMoxieModule() gate.
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
		return b.pkgModulePath != "git.smesh.lol/moxie"
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
	// A monomorphized clone carries its DEFINING package as a path prefix and
	// its type arguments as a "__" suffix. It is still the initializer it was
	// cloned from, so the mangling must not decide whether its stores to
	// package globals are legal. Mirrors stage4's isInitFuncName.
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		name = name[i+1:]
	}
	if i := strings.Index(name, "__"); i >= 0 {
		name = name[:i]
	}
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

	// Self-mutating methods must not declare variables in a loop body; such
	// declarations accumulate per-iteration scratch in the sovereign arena.
	// Applies to all code, the compiler included (runtime is exempt inside).
	b.checkSovereignLoopDecls()

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
	// Check that non-constant spawn args aren't used after the spawn call.
	b.checkSpawnMoveRestriction()
}

// checkSovereignLoopDecls rejects new variable declarations inside a loop body
// in a self-mutating method. Such declarations allocate per iteration into the
// receiver's sovereign arena, accumulating scratch that is only reclaimed at
// method return - the sovereign bloat failure. Move the loop scratch into a
// helper function so it runs in a per-call arena. Applies to all code (the
// compiler included); the runtime implements the arena system and is exempt.
func (b *builder) checkSovereignLoopDecls() {
	if !b.hasReceiver() || b.fn == nil || b.fn.Signature == nil || b.fn.Signature.Recv() == nil {
		return
	}
	recvType := b.fn.Signature.Recv().Type()
	if ptr, ok := recvType.(*types.Pointer); ok {
		recvType = ptr.Elem()
	}
	named, ok := recvType.(*types.Named)
	if !ok || !b.isSovereignNamedType(named) {
		return
	}
	if !ssaFuncWritesReceiver(b.fn) {
		return
	}
	decl, ok := b.fn.Syntax().(*ast.FuncDecl)
	if !ok || decl.Body == nil {
		return
	}
	scanSovLoopDecls(b, decl.Body.List, false)
}

func scanSovLoopDecls(b *builder, stmts []ast.Stmt, inLoop bool) {
	for _, s := range stmts {
		switch st := s.(type) {
		case *ast.DeclStmt:
			if inLoop {
				if gd, ok := st.Decl.(*ast.GenDecl); ok && gd.Tok == token.VAR {
					b.addError(st.Pos(), "moxie: variable declaration inside a loop in a self-mutating method is not allowed: move the loop scratch into a helper function")
				}
			}
		case *ast.AssignStmt:
			if inLoop && st.Tok == token.DEFINE {
				b.addError(st.Pos(), "moxie: ':=' declaration inside a loop in a self-mutating method is not allowed: move the loop scratch into a helper function")
			}
		case *ast.ForStmt:
			scanSovLoopDecls(b, st.Body.List, true)
		case *ast.RangeStmt:
			scanSovLoopDecls(b, st.Body.List, true)
		case *ast.BlockStmt:
			scanSovLoopDecls(b, st.List, inLoop)
		case *ast.IfStmt:
			scanSovLoopDecls(b, st.Body.List, inLoop)
			if st.Else != nil {
				scanSovLoopDecls(b, []ast.Stmt{st.Else}, inLoop)
			}
		case *ast.SwitchStmt:
			scanSovLoopDecls(b, st.Body.List, inLoop)
		case *ast.TypeSwitchStmt:
			scanSovLoopDecls(b, st.Body.List, inLoop)
		case *ast.SelectStmt:
			scanSovLoopDecls(b, st.Body.List, inLoop)
		case *ast.LabeledStmt:
			scanSovLoopDecls(b, []ast.Stmt{st.Stmt}, inLoop)
		case *ast.CaseClause:
			scanSovLoopDecls(b, st.Body, inLoop)
		case *ast.CommClause:
			scanSovLoopDecls(b, st.Body, inLoop)
		}
	}
}

// isIndexableType reports whether a type can be indexed: slice, array, string,
// map, pointer-to-array, or a type parameter. Mirrors stage4's isIndexable.
//
// Neither compiler used to check the operand at all: `t[0] = x` where t is a
// struct was accepted and reached LLVM, which rejected the module with
// "'%t23' defined with type '{ { ptr, i32, i32 } }' but expected
// '{ ptr, i32, i32 }'". go/ssa does not enforce indexability, so the check has
// to be here. stage4 got it first (a562f5ec); this is the bootstrap mirror.
func isIndexableType(t types.Type) bool {
	if t == nil {
		return false
	}
	switch u := t.Underlying().(type) {
	case *types.Slice, *types.Array, *types.Map:
		return true
	case *types.Basic:
		return u.Info()&types.IsString != 0
	case *types.Pointer:
		return isIndexableType(u.Elem())
	case *types.TypeParam:
		return true
	}
	return false
}

// moxieTextType reports whether T is Moxie text: string or []byte. The two
// are one MUTABLE type, so a check that compares element types has to accept
// either spelling.
func moxieTextType(T types.Type) bool {
	if T == nil {
		return false
	}
	if b, ok := T.Underlying().(*types.Basic); ok && b.Info()&types.IsString != 0 {
		return true
	}
	if sl, ok := T.Underlying().(*types.Slice); ok {
		if b, ok := sl.Elem().Underlying().(*types.Basic); ok && b.Kind() == types.Uint8 {
			return true
		}
	}
	return false
}

// moxieByteElem reports whether E is a single byte, so a string can be spread
// into a slice of it.
func moxieByteElem(E types.Type) bool {
	b, ok := E.Underlying().(*types.Basic)
	return ok && b.Kind() == types.Uint8
}

// moxieOrdered reports whether < <= > >= are defined for T: a number or text.
func moxieOrdered(T types.Type) bool {
	if T == nil {
		return false
	}
	if moxieTextType(T) {
		return true
	}
	b, ok := T.Underlying().(*types.Basic)
	return ok && b.Info()&(types.IsInteger|types.IsFloat) != 0
}

// moxieComparable reports whether == and != can be emitted for T. A slice, map
// or function has no comparison; an aggregate is comparable only when every
// element or field is. Mirrors stage4's ssaComparable.
func moxieComparable(T types.Type) bool {
	if T == nil {
		return false
	}
	switch u := T.Underlying().(type) {
	case *types.Slice, *types.Map, *types.Signature:
		return false
	case *types.Array:
		return moxieComparable(u.Elem())
	case *types.Struct:
		for i := 0; i < u.NumFields(); i++ {
			if !moxieComparable(u.Field(i).Type()) {
				return false
			}
		}
		return true
	}
	return true
}

// moxieNilable reports whether nil is a value of T.
func moxieNilable(T types.Type) bool {
	if T == nil {
		return false
	}
	switch T.Underlying().(type) {
	case *types.Pointer, *types.Slice, *types.Map, *types.Chan, *types.Signature, *types.Interface:
		return true
	}
	return moxieTextType(T)
}

// isNilLiteral reports whether a value is the untyped nil literal, with or
// without a type recorded for it.
func isNilLiteral(v ssa.Value, T types.Type) bool {
	if c, ok := v.(*ssa.Const); ok && c.IsNil() {
		return true
	}
	if T == nil {
		return false
	}
	if b, ok := T.Underlying().(*types.Basic); ok {
		return b.Kind() == types.UntypedNil
	}
	return false
}

// moxieAssignable reports whether a value of type V may be assigned to T under
// Moxie's rules: go/types' assignability plus the string/[U]int8 unification.
func moxieAssignable(V, T types.Type) bool {
	if V == nil || T == nil {
		return false
	}
	if types.AssignableTo(V, T) {
		return true
	}
	if moxieTextType(V) && moxieTextType(T) {
		return true
	}
	if b, ok := V.Underlying().(*types.Basic); ok && b.Kind() == types.UntypedNil {
		return moxieNilable(T)
	}
	return false
}

// checkLenRestriction rejects len/cap on a type that has no length. A struct
// operand took the length of the underlying slice header and clang refused the
// module. Mirrors stage4's len check.
func (b *builder) checkLenRestriction(instr *ssa.Call) {
	builtin, ok := instr.Common().Value.(*ssa.Builtin)
	if !ok || (builtin.Name() != "len" && builtin.Name() != "cap") {
		return
	}
	args := instr.Common().Args
	if len(args) != 1 || args[0] == nil {
		return
	}
	T := args[0].Type()
	switch u := T.Underlying().(type) {
	case *types.Slice, *types.Map, *types.Chan:
		return
	case *types.Basic:
		if u.Info()&types.IsString != 0 {
			return
		}
	case *types.TypeParam:
		return
	}
	if moxieTextType(T) {
		return
	}
	b.addError(instr.Pos(), "moxie: invalid argument: "+T.String()+" has no length")
}

// checkCompareRestriction rejects a comparison whose operands cannot be
// compared: nil against a value that has no nil, a mismatched pair, or an
// ordered operator on a type that has no order. go/ssa emitted the comparison
// and clang refused the module. Mirrors stage4's checkCompare.
func (b *builder) checkCompareRestriction(instr *ssa.BinOp) {
	if !isCompareOp(instr.Op) {
		return
	}
	xt := instr.X.Type()
	yt := instr.Y.Type()
	if xt == nil || yt == nil {
		return
	}
	if _, isTP := xt.(*types.TypeParam); isTP {
		return
	}
	if _, isTP := yt.(*types.TypeParam); isTP {
		return
	}
	if _, isIface := xt.Underlying().(*types.Interface); isIface {
		return
	}
	if _, isIface := yt.Underlying().(*types.Interface); isIface {
		return
	}
	ordered := instr.Op != token.EQL && instr.Op != token.NEQ
	xNil := isNilLiteral(instr.X, xt)
	yNil := isNilLiteral(instr.Y, yt)
	if xNil || yNil {
		other := yt
		if yNil {
			other = xt
		}
		if ordered || !moxieNilable(other) {
			b.addError(instr.Pos(), "moxie: invalid operation: "+instr.Op.String()+" with nil (nil cannot be compared with "+other.String()+")")
		}
		return
	}
	if moxieAssignable(xt, yt) || moxieAssignable(yt, xt) {
		if !moxieComparable(xt) || !moxieComparable(yt) {
			b.addError(instr.Pos(), "moxie: invalid operation: "+instr.Op.String()+" ("+xt.String()+" is not comparable)")
		}
		return
	}
	if ordered && (!moxieOrdered(xt) || !moxieOrdered(yt)) {
		b.addError(instr.Pos(), "moxie: invalid operation: operator "+instr.Op.String()+" not defined on "+xt.String())
		return
	}
	b.addError(instr.Pos(), "moxie: invalid operation: "+instr.Op.String()+" (mismatched types "+xt.String()+" and "+yt.String()+")")
}

// isCompareOp reports whether a token is a comparison operator.
func isCompareOp(op token.Token) bool {
	switch op {
	case token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
		return true
	}
	return false
}

// checkStoreElemRestriction rejects an assignment of a value into a slice or
// array element, a struct field or a pointer target whose type does not match.
// The store went to LLVM at the value's own type and clang refused the module
// ("invalid cast" / "type mismatch"). Mirrors stage4's store checks.
func (b *builder) checkStoreElemRestriction(instr *ssa.Store) {
	T := instr.Addr.Type()
	if p, ok := T.Underlying().(*types.Pointer); ok {
		T = p.Elem()
	} else {
		return
	}
	if instr.Val == nil || instr.Val.Type() == nil {
		return
	}
	if _, isTP := T.(*types.TypeParam); isTP {
		return
	}
	if _, isIface := T.Underlying().(*types.Interface); isIface {
		return
	}
	if moxieAssignable(instr.Val.Type(), T) {
		return
	}
	// An address that is not a slice element, array element, field or pointer
	// target is a whole-variable store, which the type checker already judges:
	// only report the shapes this check exists for.
	switch ax := instr.Addr.(type) {
	case *ssa.IndexAddr:
		_ = ax
	case *ssa.FieldAddr:
		_ = ax
	case *ssa.UnOp:
		if ax.Op != token.MUL {
			return
		}
	default:
		return
	}
	b.addError(instr.Pos(), "moxie: cannot use value of type "+instr.Val.Type().String()+" as "+T.String()+" in assignment")
}

// checkPushRestriction rejects a push whose argument cannot supply the slice's
// elements. go/ssa does not enforce the builtin's element type, so
// `push(etags, parts)` with etags []Tag and parts []string reached LLVM, which
// refused the module with "integer/byte constant must have integer/byte type"
// on the store of the element. Uses the AST because SSA cannot tell a spread
// from a plain push: both are one call with the slice-typed tail. Mirrors
// stage4's push element check.
func (b *builder) checkPushRestriction(call *ast.CallExpr) {
	if call == nil || len(call.Args) < 2 {
		return
	}
	info := b.typeInfo()
	if info == nil {
		return
	}
	ftv, ok := info.Types[call.Fun]
	if !ok || ftv.Type == nil {
		return
	}
	sig, ok := ftv.Type.(*types.Signature)
	if !ok || sig.Params().Len() != 2 || !sig.Variadic() {
		return
	}
	// The variadic parameter's own type is the []E push appends to, so its
	// element is what each pushed argument must be.
	elem, ok := sig.Params().At(1).Type().Underlying().(*types.Slice)
	if !ok {
		return
	}
	for i, arg := range call.Args {
		if i == 0 {
			continue
		}
		atv, ok := info.Types[arg]
		if !ok || atv.Type == nil {
			continue
		}
		at := atv.Type
		if b, isB := at.Underlying().(*types.Basic); isB && b.Kind() == types.UntypedNil {
			continue
		}
		if call.Ellipsis.IsValid() && i == 1 {
			// A spread supplies elements, so the argument is a slice of them -
			// or text, since string and []byte are one type.
			if sl, isSl := at.Underlying().(*types.Slice); isSl {
				if !moxieAssignable(sl.Elem(), elem.Elem()) {
					b.addError(call.Pos(), "moxie: cannot use "+at.String()+" as "+sig.Params().At(0).Type().String()+" in push (spread element type mismatch)")
				}
				continue
			}
			if moxieTextType(at) && moxieByteElem(elem.Elem()) {
				continue
			}
			b.addError(call.Pos(), "moxie: cannot use "+at.String()+" as "+sig.Params().At(0).Type().String()+" in push (spread element type mismatch)")
			continue
		}
		if moxieAssignable(at, elem.Elem()) {
			continue
		}
		b.addError(call.Pos(), "moxie: cannot use value of type "+at.String()+" as "+elem.Elem().String()+" in push to "+sig.Params().At(0).Type().String())
	}
}

// checkRangeRestriction rejects `range` over a value that cannot be ranged.
// The loop fell through every arm to the slice default, took the operand's
// first field as a slice header, and clang refused the module. Mirrors stage4's
// range check.
func (b *builder) checkRangeRestriction(rng *ast.RangeStmt) {
	if rng == nil || rng.X == nil {
		return
	}
	info := b.typeInfo()
	if info == nil {
		return
	}
	tv, ok := info.Types[rng.X]
	if !ok || tv.Type == nil {
		return
	}
	T := tv.Type
	if _, isTP := T.(*types.TypeParam); isTP {
		return
	}
	switch u := T.Underlying().(type) {
	case *types.Slice, *types.Map, *types.Array, *types.Chan, *types.Signature, *types.TypeParam:
		return
	case *types.Basic:
		if u.Info()&(types.IsInteger|types.IsString) != 0 {
			return
		}
	case *types.Pointer:
		if _, isArr := u.Elem().Underlying().(*types.Array); isArr {
			return
		}
	}
	b.addError(rng.Pos(), "moxie: cannot range over value of type "+T.String())
}

// typeInfo returns the go/types info for the function's package, or nil when
// the loader did not record one.
func (b *builder) typeInfo() *types.Info {
	if b.fn == nil || b.fn.Pkg == nil || b.fn.Pkg.Pkg == nil {
		return nil
	}
	if b.loaderPkg == nil {
		return nil
	}
	return b.loaderPkg.TypeInfo()
}

func (b *builder) checkInstrRestrictions(instr ssa.Instruction) {
	switch v := instr.(type) {
	case *ssa.IndexAddr:
		// v.X is the indexed operand. For a pointer-to-array go/ssa reports the
		// operand as the array, so only a genuinely non-indexable operand lands
		// here - which is the case this check exists for.
		//
		// This runs under checkMoxieRestrictions, which returns early for a
		// non-user package, and that is the right gate: the compiler's own tree
		// contains a handful of `len(*[N]T)`/index patterns that the toolchain
		// is allowed to write, and the app repos are where the mistake was
		// (marmot's `newTree[i]`).
		if v.X != nil && !isIndexableType(v.X.Type()) {
			b.addError(v.Pos(), "moxie: invalid operation: cannot index "+v.X.Type().String())
		}
	case *ssa.BinOp:
		b.checkCompareRestriction(v)
		// Reject + on non-numeric types (strings use | for concatenation).
		if v.Op == token.ADD {
			if basic, ok := v.X.Type().Underlying().(*types.Basic); ok {
				if basic.Info()&types.IsString != 0 {
					b.addError(v.Pos(), "moxie: '+' is not allowed for text concatenation: use | operator")
				}
			}
		}

	case *ssa.Store:
		b.checkStoreElemRestriction(v)
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

	case *ssa.SliceToArrayPointer:
		// Slices replace arrays; there is no slice-to-array machinery. This
		// conversion hands back a pointer into slice storage with a fixed
		// length the runtime does not track, so it is rejected outright.
		// Pointer-to-array puns ((*[N]T)(unsafe.Pointer(&x))) are unchanged -
		// they are *ssa.Convert, not this node.
		b.addError(v.Pos(), "moxie: slice-to-array-pointer conversion is not allowed: work in slices")

	case *ssa.Call:
		// Builtin shapes go/ssa does not judge: push's element type, and
		// len/cap on a type with no length. Both reached LLVM, which refused
		// the module.
		b.checkLenRestriction(v)
		// os.Exit is legal, but it ends the process without running deferred
		// cleanup, so whatever the program owns - a scratch tree, a lock, a
		// socket - is left behind. Warn; do not reject, a short-lived tool may
		// mean it.
		if fn := v.Common().StaticCallee(); fn != nil && fn.Pkg != nil && fn.Pkg.Pkg != nil &&
			fn.Pkg.Pkg.Path() == "os" && fn.Name() == "Exit" {
			fmt.Fprintf(os.Stderr, "  warning: os.Exit does not run deferred cleanup in %s\n", b.fn.Pkg.Pkg.Path())
		}
	}
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
		case *ast.CallExpr:
			// Ban parenthesized call targets: (f)(args) is not valid Moxie.
			// Exempt: type conversions like (*byte)(expr) where the paren wraps a type.
			if paren, ok := node.Fun.(*ast.ParenExpr); ok {
				if !isTypeExpr(paren.X) {
					b.addError(paren.Pos(), "moxie: parenthesized call target is not allowed: write f(x), not (f)(x)")
				}
			}
		case *ast.RangeStmt:
			b.checkRangeRestriction(node)
		case *ast.CallExpr:
			b.checkPushRestriction(node)
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
	// Shadowing and import-name collisions apply to ALL code - the stdlib, the
	// compiler, and user packages alike. They therefore run before the
	// user-package gate below, which otherwise makes the rules depend on
	// whether a package happens to be the build root: `moxie build ./src/math`
	// reaches this with a relative ImportPath and was checked, while the same
	// package imported as `math` was not. stage4 enforces both everywhere, so
	// gating them here was the whole of the legacy/stage4 divergence.
	c.checkScopeShadowing(pkg)
	c.checkImportNameCollisions(pkg)

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
	// A main package is user code whatever directory it happens to sit in.
	// The gate below reads the directory path, and a temp dir with no dot in
	// it exempted a user program: the same fixture was checked from
	// /tmp/moxie-tests.123 and accepted from /tmp/moxie-tests-123. stage4
	// gates on the package path, which is "main" for a program, so both
	// compilers now agree instead of the rule depending on the path's shape.
	if pkg.Pkg.Name() == "main" {
		checkPath = "main"
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
							// Name the declaration: without it the reader has to
							// bisect the package to find which var it means.
							// stage4 reports the same names.
							names := ""
							for i, nm := range vs.Names {
								if i > 0 {
									names += ", "
								}
								names += nm.Name
							}
							c.addError(vs.Pos(), "moxie: package-level var '"+names+"' with initializer is not allowed: use zero-value declaration and init()")
						}
					}
				case token.TYPE:
					for _, spec := range d.Specs {
						ts := spec.(*ast.TypeSpec)
						// An alias introduces no new named type: `type X = []T`
						// is another spelling of []T, so the rule does not
						// apply to it. stage4 exempts them the same way.
						if ts.Assign.IsValid() {
							continue
						}
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
}

// checkImportNameCollisions rejects any declaration in the package whose name is
// also an imported package name. Moxie keeps one binding per name: an import and
// a variable of the same name are two bindings for one identifier, and which one
// a given reference means then depends on the file it sits in. Alias the import
// (import stdio "io") or rename the declaration.
//
// Three things are reserved, not one: the name an import binds, every exported
// name a dot import puts in scope, and - within a single file - each name once.
// A dot import binds no single name, so treating it as binding nothing (as this
// did) left every name it injects invisible to the rule: a declaration could
// collide with it and nothing reported the conflict. Two imports binding one
// name in one file is the same collision, one file up.
func (c *compilerContext) checkImportNameCollisions(pkg *loader.Package) {
	info := pkg.TypeInfo()
	if info == nil {
		return
	}
	imported := map[string]bool{}
	for _, f := range pkg.Files {
		if f == nil {
			continue
		}
		fileImports := map[string]token.Pos{}
		for _, spec := range f.Imports {
			// An explicit name wins; otherwise the imported package's own
			// name, which is not necessarily the path tail. Blank imports bind
			// no name.
			var obj types.Object
			if spec.Name != nil {
				obj = info.Defs[spec.Name]
			} else {
				obj = info.Implicits[spec]
			}
			if obj == nil {
				continue
			}
			if _, isPkg := obj.(*types.PkgName); !isPkg {
				continue
			}
			// A blank import defines the blank identifier, which go/types puts
			// in Defs as an object named "_". "_" is not a binding, so treating
			// it as an imported package name flagged every blank declaration in
			// the package (210 of them in one generated syscall file).
			if obj.Name() == "_" {
				continue
			}
			if obj.Name() == "." {
				// Reserve every exported name the dot import injects - only
				// exported ones: Scope().Names() also returns unexported
				// helpers and the __moxie_* builtins the loader injects into
				// the imported package itself, and a dot import brings in
				// neither.
				if imp := dotImportTarget(pkg, spec); imp != nil && imp.Scope() != nil {
					for _, n := range imp.Scope().Names() {
						if !token.IsExported(n) {
							continue
						}
						imported[n] = true
					}
				}
				continue
			}
			if _, dup := fileImports[obj.Name()]; dup {
				c.addError(spec.Pos(), "moxie: '"+obj.Name()+"' is imported twice in one file: alias one of the imports")
			}
			fileImports[obj.Name()] = spec.Pos()
			imported[obj.Name()] = true
		}
	}
	if len(imported) == 0 {
		return
	}

	type collision struct {
		pos  token.Pos
		name string
	}
	var hits []collision
	// Only lexical bindings collide with an import name: package scope and the
	// scopes inside function bodies. A method name or a struct field name is
	// bound to a type, not to a scope, so `func (c nistCurve) ecdh(...)` does
	// not collide with an imported `ecdh` package.
	seen := map[token.Pos]bool{}
	flag := func(scope *types.Scope) {
		if scope == nil {
			return
		}
		for _, name := range scope.Names() {
			if name == "_" || !imported[name] {
				continue
			}
			obj := scope.Lookup(name)
			if obj == nil {
				continue
			}
			// The import binding itself is not a collision.
			if _, isPkg := obj.(*types.PkgName); isPkg {
				continue
			}
			// Nor is a name that came in through a dot import: those objects
			// live in the file scope but belong to the imported package, so
			// they are that package's binding, not a second one here. Without
			// this every name a dot import injects reports as a collision with
			// itself.
			if op := obj.Pkg(); op != nil && op != pkg.Pkg {
				continue
			}
			if seen[obj.Pos()] {
				continue
			}
			seen[obj.Pos()] = true
			hits = append(hits, collision{pos: obj.Pos(), name: name})
		}
	}
	pkgScope := pkg.Pkg.Scope()
	flag(pkgScope)
	for _, scope := range info.Scopes {
		if scope == nil || scope == pkgScope {
			continue
		}
		flag(scope)
	}
	// info.Scopes is a map, so sort before reporting: diagnostics must not
	// depend on map iteration order.
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].pos != hits[j].pos {
			return hits[i].pos < hits[j].pos
		}
		return hits[i].name < hits[j].name
	})
	for _, h := range hits {
		c.addError(h.pos, "moxie: '"+h.name+"' collides with an imported package name: alias the import or rename the declaration")
	}
}

// dotImportTarget returns the package a dot import names, matched by its import
// path. info is not used: a dot import's PkgName is named "." and go/types may
// record it in Defs or Implicits depending on how it was written, while the
// path is always on the spec.
func dotImportTarget(pkg *loader.Package, spec *ast.ImportSpec) *types.Package {
	if spec == nil || spec.Path == nil {
		return nil
	}
	path, err := strconv.Unquote(spec.Path.Value)
	if err != nil {
		path = spec.Path.Value
	}
	for _, imp := range pkg.Pkg.Imports() {
		if imp.Path() == path {
			return imp
		}
	}
	return nil
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
		// The argument is a string expression: a literal, or literals joined
		// with | or + (wrapStringExpr normalizes | to + and wraps the whole
		// expression in one []byte conversion).
		if !isStringLiteralExpr(call.Args[0]) {
			return false
		}
	}
	return true
}

// isStringLiteralExpr reports whether expr is a string literal, possibly
// joined with + or | and parenthesized.
func isStringLiteralExpr(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.BasicLit:
		return e.Kind == token.STRING
	case *ast.BinaryExpr:
		return (e.Op == token.ADD || e.Op == token.OR) &&
			isStringLiteralExpr(e.X) && isStringLiteralExpr(e.Y)
	case *ast.ParenExpr:
		return isStringLiteralExpr(e.X)
	}
	return false
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
