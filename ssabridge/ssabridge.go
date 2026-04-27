// Package ssabridge translates go/ssa IR into mxssa IR.
// It is a temporary shim for B3: the compiler receives ssa.Package from the
// loader (which still uses go/types), and the bridge converts it to
// mxssa.Package so that migrated compiler files can consume native types.
// Deleted when all compiler files have migrated and the loader produces
// mxssa directly.
package ssabridge

import (
	"go/token"
	"go/types"
	"unsafe"

	"moxie/mxssa"
	"moxie/typecheck"
	tcbridge "moxie/typecheck/bridge"

	"golang.org/x/tools/go/ssa"
)

// Translator converts an ssa.Program into an mxssa.Program.
type Translator struct {
	prog     *mxssa.Program
	ssaProg  *ssa.Program // kept so the bridge can hide method-set queries
	imp      *tcbridge.Importer
	fset     *token.FileSet
	funcs    map[*ssa.Function]*mxssa.Function
	globals  map[*ssa.Global]*mxssa.Global
	pkgs     map[*ssa.Package]*mxssa.Package
	revTypes   map[typecheck.Type]types.Type
	revFuncs   map[*mxssa.Function]*ssa.Function
	revGlobals map[*mxssa.Global]*ssa.Global
	// typeCache maps go/types.Type (by pointer) to its canonical typecheck.Type.
	// Ensures the same TC type pointer is returned for the same go/types type
	// so that revTypes lookups remain consistent.
	typeCache map[uintptr]typecheck.Type
}

// Importer returns the bridge type importer for cross-validation.
func (t *Translator) Importer() *tcbridge.Importer { return t.imp }

// LookupFunction returns the mxssa.Function for an ssa.Function, or nil.
func (t *Translator) LookupFunction(f *ssa.Function) *mxssa.Function { return t.funcs[f] }

// LookupGlobal returns the mxssa.Global for an ssa.Global, or nil.
func (t *Translator) LookupGlobal(g *ssa.Global) *mxssa.Global { return t.globals[g] }

// LookupPackage returns the mxssa.Package for an ssa.Package, or nil.
func (t *Translator) LookupPackage(p *ssa.Package) *mxssa.Package { return t.pkgs[p] }

// EnsureFunction ensures f has a full mxssa translation (shell + body).
// The value() helper may register only a shell for cross-package function
// references; this fills in the body if it is missing.
// Returns a shell for any function, including external ones (no SSA blocks).
// Creates a shell if not yet registered.
func (t *Translator) EnsureFunction(f *ssa.Function) *mxssa.Function {
	if f == nil {
		return nil
	}
	mf, ok := t.funcs[f]
	if !ok {
		var pkg *mxssa.Package
		if f.Package() != nil {
			pkg = t.ensurePackage(f.Package())
		}
		mf = t.translateFunctionShell(f, pkg)
	}
	if len(f.Blocks) == 0 {
		// External/stub — shell-only is sufficient.
		return mf
	}
	if len(mf.Blocks) > 0 {
		// Already fully translated. Backfill any params/freevars added since
		// the shell was created — go/ssa populates f.Params lazily during
		// pkg.Build(), so cross-package shells made earlier may have empty
		// Params even though f.Params is now set. Same for FreeVars.
		t.backfillShellMembers(f, mf)
		return mf
	}
	// Same backfill before translating the body — body translation indexes
	// into mf.Params for parameter-value lookups.
	t.backfillShellMembers(f, mf)
	// mf exists (either just created or a shell from value()) but has no blocks.
	t.translateFunctionBody(f, mf)
	return mf
}

// backfillShellMembers appends any parameters/freevars present on f but
// missing from mf. Handles the lazy-population race: a shell created during
// cross-package translation captured f.Params at a time when go/ssa hadn't
// built f's package yet, so f.Params was empty.
func (t *Translator) backfillShellMembers(f *ssa.Function, mf *mxssa.Function) {
	for i := len(mf.Params); i < len(f.Params); i++ {
		p := f.Params[i]
		mp := mxssa.NewParameter(p.Name(), t.typ(p.Type()), mf)
		mp.SetPos(p.Pos())
		if pObj := p.Object(); pObj != nil {
			if tcObj := tcbridge.TranslateObject(t.imp, pObj); tcObj != nil {
				if tcVar, ok := tcObj.(*typecheck.Var); ok {
					mp.SetObject(tcVar)
				}
			}
		}
		mf.Params = append(mf.Params, mp)
	}
	for i := len(mf.FreeVars); i < len(f.FreeVars); i++ {
		fv := f.FreeVars[i]
		mfv := mxssa.NewFreeVar(fv.Name(), t.typ(fv.Type()), mf)
		mfv.SetPos(fv.Pos())
		mf.FreeVars = append(mf.FreeVars, mfv)
	}
}

// Prepared holds the bridge state for one CompilePackage invocation.
// PrepareCompile builds it; CompilePackage consumes it. Hides go/ssa from
// the compiler so it can drop the import.
type Prepared struct {
	MxProg       *mxssa.Program
	Translator   *Translator
	MxPkg        *mxssa.Package    // current package
	GoRuntimePkg *types.Package    // runtime's go/types package (still needed for createDIType chain)
}

// PrepareCompile is the new entry point for the compiler. It runs ssa.Build,
// constructs the bridge, and pre-translates runtime+internal/task. The
// returned Prepared lets CompilePackage operate without importing go/ssa.
func PrepareCompile(ssaPkg *ssa.Package) *Prepared {
	ssaPkg.Build()
	typePkgs := map[string]*types.Package{}
	for _, p := range ssaPkg.Prog.AllPackages() {
		typePkgs[p.Pkg.Path()] = p.Pkg
	}
	mxProg, mxTrans := Translate(ssaPkg.Prog.Fset, ssaPkg.Prog, typePkgs)
	mxTrans.TranslatePackage(ssaPkg)
	for _, importPath := range []string{"runtime", "internal/task"} {
		if rt := ssaPkg.Prog.ImportedPackage(importPath); rt != nil {
			mxTrans.TranslatePackage(rt)
		}
	}
	var mxPkg *mxssa.Package
	if mxP := mxProg.ImportedPackage(ssaPkg.Pkg.Path()); mxP != nil {
		mxPkg = mxP
	}
	var runtimePkg *types.Package
	if rt := ssaPkg.Prog.ImportedPackage("runtime"); rt != nil {
		runtimePkg = rt.Pkg
	}
	return &Prepared{
		MxProg:       mxProg,
		Translator:   mxTrans,
		MxPkg:        mxPkg,
		GoRuntimePkg: runtimePkg,
	}
}

// Translate converts an entire ssa.Program to mxssa.Program.
// The typePkgs map provides go/types packages for type translation.
func Translate(fset *token.FileSet, ssaProg *ssa.Program, typePkgs map[string]*types.Package) (*mxssa.Program, *Translator) {
	prog := mxssa.NewProgram(fset)
	imp := tcbridge.New(typePkgs)
	t := &Translator{
		prog:      prog,
		ssaProg:   ssaProg,
		imp:       imp,
		fset:      fset,
		funcs:     map[*ssa.Function]*mxssa.Function{},
		globals:   map[*ssa.Global]*mxssa.Global{},
		pkgs:      map[*ssa.Package]*mxssa.Package{},
		revTypes:   map[typecheck.Type]types.Type{},
		revFuncs:   map[*mxssa.Function]*ssa.Function{},
		revGlobals: map[*mxssa.Global]*ssa.Global{},
		typeCache: map[uintptr]typecheck.Type{},
	}
	return prog, t
}

// MethodSetItem is one entry of a type's method set.
type MethodSetItem struct {
	// Obj is the go/types method object — kept for compatibility with
	// callers that need the qualified method name (interface.go's
	// getMethodSignature / getMethodsString).
	Obj *types.Func
	// Func is the mxssa.Function that implements this method.
	Func *mxssa.Function
}

// MethodSet returns the method set of typ as a list of (Obj, Func) pairs.
// Hides go/ssa.Program.MethodSets behind the bridge so the compiler doesn't
// need to import go/ssa just for these queries. Takes go/types.Type to
// avoid forcing every caller through a typecheck conversion (interface.go's
// getTypeCode chain still operates on go/types).
func (t *Translator) MethodSet(typ types.Type) []MethodSetItem {
	ms := t.ssaProg.MethodSets.MethodSet(typ)
	var items []MethodSetItem
	for i := 0; i < ms.Len(); i++ {
		method := ms.At(i)
		obj, _ := method.Obj().(*types.Func)
		ssaFn := t.ssaProg.MethodValue(method)
		var mxFn *mxssa.Function
		if ssaFn != nil {
			mxFn = t.EnsureFunction(ssaFn)
		}
		items = append(items, MethodSetItem{Obj: obj, Func: mxFn})
	}
	return items
}

// MethodsForType returns the methods on a named type, including both
// value-receiver and pointer-receiver methods. Hides the go/ssa.Program
// method-set queries from the compiler so it can stop importing go/ssa.
//
// MethodSet(*T) includes value-receiver methods of T (since *T can call
// them via auto-deref), so iterating both T and *T method sets produces
// duplicates; deduplicate by underlying *ssa.Function so the same method
// isn't compiled twice (which would trigger LLVM "redeclared" errors).
func (t *Translator) MethodsForType(typ typecheck.Type) []*mxssa.Function {
	goType := t.revTypes[typ]
	if goType == nil {
		return nil
	}
	seen := map[*ssa.Function]bool{}
	var fns []*mxssa.Function
	add := func(t2 types.Type) {
		ms := t.ssaProg.MethodSets.MethodSet(t2)
		for i := 0; i < ms.Len(); i++ {
			method := ms.At(i)
			ssaFn := t.ssaProg.MethodValue(method)
			if ssaFn == nil || seen[ssaFn] {
				continue
			}
			seen[ssaFn] = true
			fns = append(fns, t.EnsureFunction(ssaFn))
		}
	}
	add(goType)
	add(types.NewPointer(goType))
	return fns
}

// TranslatePackage converts one ssa.Package to mxssa.Package.
// Must be called after the ssa package is built (ssaPkg.Build()).
// Idempotent: re-populates Members if the package was previously registered
// only as a shell (via ensurePackage).
func (t *Translator) TranslatePackage(ssaPkg *ssa.Package) *mxssa.Package {
	p, existed := t.pkgs[ssaPkg]
	if !existed {
		tcPkg, _ := t.imp.Import(ssaPkg.Pkg.Path())
		if tcPkg == nil {
			tcPkg = typecheck.NewPackage(ssaPkg.Pkg.Path(), ssaPkg.Pkg.Name())
		}
		// Populate imports so checks like packageImportsUnsafeTC can find them.
		if len(tcPkg.Imports()) == 0 {
			var tcImps []*typecheck.Package
			for _, gimp := range ssaPkg.Pkg.Imports() {
				if tcImp, _ := t.imp.Import(gimp.Path()); tcImp != nil {
					tcImps = append(tcImps, tcImp)
				}
			}
			if len(tcImps) > 0 {
				tcPkg.SetImports(tcImps)
			}
		}
		p = &mxssa.Package{
			Prog:    t.prog,
			Pkg:     tcPkg,
			Members: map[string]mxssa.Member{},
		}
		t.pkgs[ssaPkg] = p
		t.prog.RegisterPackage(p)
	}
	// Members may already be partially populated (shells from cross-package
	// references). Skip if the package is fully populated; otherwise add any
	// missing entries from ssaPkg.Members.
	if existed && len(p.Members) >= len(ssaPkg.Members) {
		return p
	}

	for name, m := range ssaPkg.Members {
		if _, ok := p.Members[name]; ok {
			continue // shell already exists
		}
		switch m := m.(type) {
		case *ssa.Function:
			fn := t.translateFunctionShell(m, p)
			p.Members[name] = fn
		case *ssa.Global:
			g := t.translateGlobal(m, p)
			p.Members[name] = g
		case *ssa.Type:
			tn := t.translateTypeMember(m, p)
			p.Members[name] = tn
		case *ssa.NamedConst:
			nc := t.translateNamedConst(m, p)
			p.Members[name] = nc
		}
	}

	// Build function bodies after all members registered (cross-references).
	for name, m := range ssaPkg.Members {
		if f, ok := m.(*ssa.Function); ok {
			if fn, ok := p.Members[name].(*mxssa.Function); ok {
				t.translateFunctionBody(f, fn)
			}
		}
	}

	return p
}

func typePtr(t types.Type) uintptr {
	type iface struct{ typ, ptr uintptr }
	return (*iface)(unsafe.Pointer(&t)).ptr
}

func (t *Translator) typ(gt types.Type) typecheck.Type {
	if gt == nil {
		return nil
	}
	// Use a global cache keyed by pointer identity to ensure the same go/types
	// type always maps to the same typecheck.Type pointer. This keeps revTypes
	// consistent: multiple TranslateType calls for the same type return
	// different pointers, but we always use the first one.
	key := typePtr(gt)
	if cached, ok := t.typeCache[key]; ok {
		return cached
	}
	tc := tcbridge.TranslateType(t.imp, gt)
	if tc != nil {
		t.typeCache[key] = tc
		t.revTypes[tc] = gt
		if u := tc.Underlying(); u != nil && u != tc {
			if _, exists := t.revTypes[u]; !exists {
				if gu := gt.Underlying(); gu != nil {
					t.revTypes[u] = gu
				}
			}
		}
	}
	return tc
}

func (t *Translator) ReverseType(tc typecheck.Type) types.Type { return t.revTypes[tc] }

// ReverseGlobal returns the ssa.Global for an mxssa.Global, or nil.
func (t *Translator) ReverseGlobal(mg *mxssa.Global) *ssa.Global { return t.revGlobals[mg] }

// ForwardType converts a go/types.Type to its typecheck equivalent, using the
// translator's cache so the same go/types pointer always maps to the same
// typecheck.Type pointer. Mirror of ReverseType for the forward direction.
func (t *Translator) ForwardType(gt types.Type) typecheck.Type { return t.typ(gt) }

func (t *Translator) ReverseFunction(fn *mxssa.Function) *ssa.Function { return t.revFuncs[fn] }

// DomPreorder returns the function's basic blocks in dominator-tree preorder
// (definitions before uses). Computed via the underlying ssa.Function's
// existing dominator analysis, then translated to mxssa blocks by index.
// Returns nil if no SSA-side function exists for this mxssa.Function.
func (t *Translator) DomPreorder(fn *mxssa.Function) []*mxssa.BasicBlock {
	goFn := t.revFuncs[fn]
	if goFn == nil {
		return nil
	}
	preorder := goFn.DomPreorder()
	out := make([]*mxssa.BasicBlock, 0, len(preorder))
	for _, b := range preorder {
		if b.Index < len(fn.Blocks) {
			out = append(out, fn.Blocks[b.Index])
		}
	}
	return out
}

func (t *Translator) translateFunctionShell(f *ssa.Function, pkg *mxssa.Package) *mxssa.Function {
	if fn, ok := t.funcs[f]; ok {
		return fn
	}
	var sig *typecheck.Signature
	if f.Signature != nil {
		if s, ok := t.typ(f.Signature).(*typecheck.Signature); ok {
			sig = s
		}
	}
	fn := mxssa.NewFunction(f.Name(), sig, f.Synthetic, pkg, t.prog)
	fn.SetPos(f.Pos())
	if syn := f.Syntax(); syn != nil {
		fn.SyntaxNode = syn
	}
	for _, p := range f.Params {
		mp := mxssa.NewParameter(p.Name(), t.typ(p.Type()), fn)
		mp.SetPos(p.Pos())
		// Populate Object so getLocalVariable can dispatch on *typecheck.Var
		// without having to bridge through *types.Var.
		if pObj := p.Object(); pObj != nil {
			if tcObj := tcbridge.TranslateObject(t.imp, pObj); tcObj != nil {
				if tcVar, ok := tcObj.(*typecheck.Var); ok {
					mp.SetObject(tcVar)
				}
			}
		}
		fn.Params = append(fn.Params, mp)
	}
	for _, fv := range f.FreeVars {
		mfv := mxssa.NewFreeVar(fv.Name(), t.typ(fv.Type()), fn)
		mfv.SetPos(fv.Pos())
		fn.FreeVars = append(fn.FreeVars, mfv)
	}
	t.funcs[f] = fn
	t.revFuncs[fn] = f
	return fn
}

func (t *Translator) ensurePackage(ssaPkg *ssa.Package) *mxssa.Package {
	if p, ok := t.pkgs[ssaPkg]; ok {
		return p
	}
	tcPkg, _ := t.imp.Import(ssaPkg.Pkg.Path())
	if tcPkg == nil {
		tcPkg = typecheck.NewPackage(ssaPkg.Pkg.Path(), ssaPkg.Pkg.Name())
	}
	if len(tcPkg.Imports()) == 0 {
		var tcImps []*typecheck.Package
		for _, gimp := range ssaPkg.Pkg.Imports() {
			if tcImp, _ := t.imp.Import(gimp.Path()); tcImp != nil {
				tcImps = append(tcImps, tcImp)
			}
		}
		if len(tcImps) > 0 {
			tcPkg.SetImports(tcImps)
		}
	}
	p := &mxssa.Package{
		Prog:    t.prog,
		Pkg:     tcPkg,
		Members: map[string]mxssa.Member{},
	}
	t.pkgs[ssaPkg] = p
	t.prog.RegisterPackage(p)
	return p
}

func (t *Translator) translateGlobal(g *ssa.Global, pkg *mxssa.Package) *mxssa.Global {
	if mg, ok := t.globals[g]; ok {
		return mg
	}
	mg := mxssa.NewGlobal(g.Name(), t.typ(g.Type()), pkg)
	mg.SetPos(g.Pos())
	t.globals[g] = mg
	t.revGlobals[mg] = g
	return mg
}

func (t *Translator) translateTypeMember(m *ssa.Type, pkg *mxssa.Package) *mxssa.Type_ {
	return mxssa.NewType_(m.Name(), t.typ(m.Type()), pkg)
}

func (t *Translator) translateNamedConst(m *ssa.NamedConst, pkg *mxssa.Package) *mxssa.NamedConst {
	return mxssa.NewNamedConst(m.Name(), t.typ(m.Type()), m.Value.Value, pkg)
}

// translateFunctionBody fills in blocks and instructions.
// Params and freevars are created in translateFunctionShell.
func (t *Translator) translateFunctionBody(sf *ssa.Function, mf *mxssa.Function) {
	if len(sf.Blocks) == 0 {
		return // external function, no body
	}
	if len(mf.Blocks) > 0 {
		return // already translated; calling again would duplicate blocks
	}
	// Backfill any params/freevars that were lazily populated on sf after
	// the shell was created (cross-package references made the shell early
	// when sf.Params was still empty).
	t.backfillShellMembers(sf, mf)

	// Create blocks first (instructions reference blocks by index).
	blockMap := map[*ssa.BasicBlock]*mxssa.BasicBlock{}
	for _, b := range sf.Blocks {
		mb := mxssa.NewBasicBlock(mf, b.Comment)
		blockMap[b] = mb
	}

	// Wire preds/succs.
	for _, b := range sf.Blocks {
		mb := blockMap[b]
		for _, pred := range b.Preds {
			mb.Preds = append(mb.Preds, blockMap[pred])
		}
		for _, succ := range b.Succs {
			mb.Succs = append(mb.Succs, blockMap[succ])
		}
	}

	if sf.Recover != nil {
		mf.Recover = blockMap[sf.Recover]
	}

	// Value map: ssa.Value → mxssa.Value.
	// Pre-populate with params and freevars.
	valMap := map[ssa.Value]mxssa.Value{}
	for i, p := range sf.Params {
		valMap[p] = mf.Params[i]
	}
	for i, fv := range sf.FreeVars {
		valMap[fv] = mf.FreeVars[i]
	}

	// Translate instructions in dominator-tree preorder so that definitions
	// are visited before their uses (SSA dominance property).
	// Phi edges are deferred because they can reference values from predecessor
	// blocks that aren't dominators (e.g. loop back-edges).
	type deferredPhi struct {
		phi   *mxssa.Phi
		edges []ssa.Value
	}
	var deferred []deferredPhi
	for _, b := range sf.DomPreorder() {
		mb := blockMap[b]
		for _, instr := range b.Instrs {
			if phiInstr, ok := instr.(*ssa.Phi); ok {
				edges := make([]mxssa.Value, len(phiInstr.Edges))
				p := mxssa.NewPhi(t.typ(phiInstr.Type()), phiInstr.Comment, edges, mb)
				p.SetPos(phiInstr.Pos())
				p.SetName(phiInstr.Name())
				mb.Instrs = append(mb.Instrs, p)
				valMap[phiInstr] = p
				deferred = append(deferred, deferredPhi{p, phiInstr.Edges})
				continue
			}
			mi := t.translateInstruction(instr, mb, blockMap, valMap)
			if mi != nil {
				mb.Instrs = append(mb.Instrs, mi)
				if v, ok := instr.(ssa.Value); ok {
					if mv, ok := mi.(mxssa.Value); ok {
						valMap[v] = mv
					}
				}
			}
		}
	}
	for _, dp := range deferred {
		for j, e := range dp.edges {
			dp.phi.Edges[j] = t.value(e, valMap)
		}
	}

	// Anonymous functions.
	for _, af := range sf.AnonFuncs {
		maf := t.translateFunctionShell(af, mf.Pkg)
		t.translateFunctionBody(af, maf)
		mf.AnonFuncs = append(mf.AnonFuncs, maf)
	}

	// Wire referrers so Value.Referrers() returns the correct usage list.
	mf.WireReferrers()
}

// value translates an ssa.Value to mxssa.Value.
// Returns the cached translation or creates one on demand.
func (t *Translator) value(v ssa.Value, valMap map[ssa.Value]mxssa.Value) mxssa.Value {
	if v == nil {
		return nil
	}
	if mv, ok := valMap[v]; ok {
		return mv
	}
	switch v := v.(type) {
	case *ssa.Const:
		mc := mxssa.NewConst(v.Value, t.typ(v.Type()))
		valMap[v] = mc
		return mc
	case *ssa.Global:
		pkg := t.pkgs[v.Package()]
		if pkg == nil && v.Package() != nil {
			pkg = t.ensurePackage(v.Package())
		}
		mg := t.translateGlobal(v, pkg)
		valMap[v] = mg
		return mg
	case *ssa.Function:
		var pkg *mxssa.Package
		if v.Package() != nil {
			pkg = t.pkgs[v.Package()]
			if pkg == nil {
				pkg = t.ensurePackage(v.Package())
			}
		}
		mf := t.translateFunctionShell(v, pkg)
		valMap[v] = mf
		return mf
	case *ssa.Builtin:
		mb := mxssa.NewBuiltin(v.Name())
		valMap[v] = mb
		return mb
	}
	// If we get here the value was defined by an instruction that hasn't
	// been translated yet, which shouldn't happen in a well-ordered walk.
	// Return a placeholder const.
	mc := mxssa.NewConst(nil, t.typ(v.Type()))
	valMap[v] = mc
	return mc
}

func (t *Translator) translateInstruction(
	instr ssa.Instruction,
	mb *mxssa.BasicBlock,
	blockMap map[*ssa.BasicBlock]*mxssa.BasicBlock,
	valMap map[ssa.Value]mxssa.Value,
) mxssa.Instruction {
	v := func(sv ssa.Value) mxssa.Value { return t.value(sv, valMap) }

	switch i := instr.(type) {
	case *ssa.Alloc:
		a := mxssa.NewAlloc(t.typ(i.Type()), i.Heap, i.Comment, mb)
		a.SetPos(i.Pos())
		a.SetName(i.Name())
		return a

	case *ssa.Phi:
		return nil // handled in deferred pass

	case *ssa.Call:
		cc := t.translateCallCommon(&i.Call, valMap)
		c := mxssa.NewCall(t.typ(i.Type()), cc, mb)
		c.SetPos(i.Pos())
		c.SetName(i.Name())
		return c

	case *ssa.BinOp:
		b := mxssa.NewBinOp(t.typ(i.Type()), i.Op, v(i.X), v(i.Y), mb)
		b.SetPos(i.Pos())
		b.SetName(i.Name())
		return b

	case *ssa.UnOp:
		u := mxssa.NewUnOp(t.typ(i.Type()), i.Op, v(i.X), i.CommaOk, mb)
		u.SetPos(i.Pos())
		u.SetName(i.Name())
		return u

	case *ssa.ChangeType:
		ct := mxssa.NewChangeType(t.typ(i.Type()), v(i.X), mb)
		ct.SetPos(i.Pos())
		ct.SetName(i.Name())
		return ct

	case *ssa.Convert:
		cv := mxssa.NewConvert(t.typ(i.Type()), v(i.X), mb)
		cv.SetPos(i.Pos())
		cv.SetName(i.Name())
		return cv

	case *ssa.ChangeInterface:
		ci := mxssa.NewChangeInterface(t.typ(i.Type()), v(i.X), mb)
		ci.SetPos(i.Pos())
		ci.SetName(i.Name())
		return ci

	case *ssa.SliceToArrayPointer:
		s := mxssa.NewSliceToArrayPointer(t.typ(i.Type()), v(i.X), mb)
		s.SetPos(i.Pos())
		s.SetName(i.Name())
		return s

	case *ssa.MakeInterface:
		mi := mxssa.NewMakeInterface(t.typ(i.Type()), v(i.X), mb)
		mi.SetPos(i.Pos())
		mi.SetName(i.Name())
		return mi

	case *ssa.MakeClosure:
		bindings := make([]mxssa.Value, len(i.Bindings))
		for j, b := range i.Bindings {
			bindings[j] = v(b)
		}
		mc := mxssa.NewMakeClosure(t.typ(i.Type()), v(i.Fn), bindings, mb)
		mc.SetPos(i.Pos())
		mc.SetName(i.Name())
		return mc

	case *ssa.MakeMap:
		var reserve mxssa.Value
		if i.Reserve != nil {
			reserve = v(i.Reserve)
		}
		mm := mxssa.NewMakeMap(t.typ(i.Type()), reserve, mb)
		mm.SetPos(i.Pos())
		mm.SetName(i.Name())
		return mm

	case *ssa.MakeChan:
		var sz mxssa.Value
		if i.Size != nil {
			sz = v(i.Size)
		}
		mc := mxssa.NewMakeChan(t.typ(i.Type()), sz, mb)
		mc.SetPos(i.Pos())
		mc.SetName(i.Name())
		return mc

	case *ssa.MakeSlice:
		var cap mxssa.Value
		if i.Cap != nil {
			cap = v(i.Cap)
		}
		ms := mxssa.NewMakeSlice(t.typ(i.Type()), v(i.Len), cap, mb)
		ms.SetPos(i.Pos())
		ms.SetName(i.Name())
		return ms

	case *ssa.Slice:
		var low, high, max mxssa.Value
		if i.Low != nil {
			low = v(i.Low)
		}
		if i.High != nil {
			high = v(i.High)
		}
		if i.Max != nil {
			max = v(i.Max)
		}
		s := mxssa.NewSlice(t.typ(i.Type()), v(i.X), low, high, max, mb)
		s.SetPos(i.Pos())
		s.SetName(i.Name())
		return s

	case *ssa.FieldAddr:
		fa := mxssa.NewFieldAddr(t.typ(i.Type()), v(i.X), i.Field, mb)
		fa.SetPos(i.Pos())
		fa.SetName(i.Name())
		return fa

	case *ssa.Field:
		f := mxssa.NewField(t.typ(i.Type()), v(i.X), i.Field, mb)
		f.SetPos(i.Pos())
		f.SetName(i.Name())
		return f

	case *ssa.IndexAddr:
		ia := mxssa.NewIndexAddr(t.typ(i.Type()), v(i.X), v(i.Index), mb)
		ia.SetPos(i.Pos())
		ia.SetName(i.Name())
		return ia

	case *ssa.Index:
		idx := mxssa.NewIndex(t.typ(i.Type()), v(i.X), v(i.Index), mb)
		idx.SetPos(i.Pos())
		idx.SetName(i.Name())
		return idx

	case *ssa.Lookup:
		l := mxssa.NewLookup(t.typ(i.Type()), v(i.X), v(i.Index), i.CommaOk, mb)
		l.SetPos(i.Pos())
		l.SetName(i.Name())
		return l

	case *ssa.Select:
		states := make([]*mxssa.SelectState, len(i.States))
		for j, s := range i.States {
			dir := token.ARROW // recv
			if s.Dir == types.SendOnly {
				dir = token.CHAN // send — mxssa uses token.Token, not types.ChanDir
			}
			var send mxssa.Value
			if s.Send != nil {
				send = v(s.Send)
			}
			states[j] = &mxssa.SelectState{
				Dir:  dir,
				Chan: v(s.Chan),
				Send: send,
				Pos:  s.Pos,
			}
		}
		sel := mxssa.NewSelect(t.typ(i.Type()), states, i.Blocking, mb)
		sel.SetPos(i.Pos())
		sel.SetName(i.Name())
		return sel

	case *ssa.Range:
		r := mxssa.NewRange(t.typ(i.Type()), v(i.X), mb)
		r.SetPos(i.Pos())
		r.SetName(i.Name())
		return r

	case *ssa.Next:
		n := mxssa.NewNext(t.typ(i.Type()), v(i.Iter), i.IsString, mb)
		n.SetPos(i.Pos())
		n.SetName(i.Name())
		return n

	case *ssa.TypeAssert:
		ta := mxssa.NewTypeAssert(t.typ(i.Type()), v(i.X), t.typ(i.AssertedType), i.CommaOk, mb)
		ta.SetPos(i.Pos())
		ta.SetName(i.Name())
		return ta

	case *ssa.Extract:
		e := mxssa.NewExtract(t.typ(i.Type()), v(i.Tuple), i.Index, mb)
		e.SetPos(i.Pos())
		e.SetName(i.Name())
		return e

	// Terminators.
	case *ssa.Jump:
		return mxssa.NewJump("", mb)

	case *ssa.If:
		return mxssa.NewIf(v(i.Cond), mb)

	case *ssa.Return:
		results := make([]mxssa.Value, len(i.Results))
		for j, r := range i.Results {
			results[j] = v(r)
		}
		ret := mxssa.NewReturn(results, mb)
		ret.SetPos(i.Pos())
		return ret

	case *ssa.RunDefers:
		return mxssa.NewRunDefers(mb)

	case *ssa.Panic:
		p := mxssa.NewPanic(v(i.X), mb)
		p.SetPos(i.Pos())
		return p

	// Side-effecting.
	case *ssa.Go:
		cc := t.translateCallCommon(&i.Call, valMap)
		g := mxssa.NewGo(cc, mb)
		g.SetPos(i.Pos())
		return g

	case *ssa.Defer:
		cc := t.translateCallCommon(&i.Call, valMap)
		d := mxssa.NewDefer(cc, mb)
		d.SetPos(i.Pos())
		return d

	case *ssa.Send:
		s := mxssa.NewSend(v(i.Chan), v(i.X), mb)
		s.SetPos(i.Pos())
		return s

	case *ssa.Store:
		s := mxssa.NewStore(v(i.Addr), v(i.Val), mb)
		s.SetPos(i.Pos())
		return s

	case *ssa.MapUpdate:
		m := mxssa.NewMapUpdate(v(i.Map), v(i.Key), v(i.Value), mb)
		m.SetPos(i.Pos())
		return m

	case *ssa.DebugRef:
		var obj typecheck.Object
		if i.Object() != nil {
			// Translate the debug object if possible.
			if o := i.Object(); o != nil {
				obj = tcbridge.TranslateObject(t.imp, o)
			}
		}
		d := mxssa.NewDebugRef(v(i.X), obj, i.IsAddr, mb)
		return d
	}

	return nil
}

func (t *Translator) translateCallCommon(cc *ssa.CallCommon, valMap map[ssa.Value]mxssa.Value) mxssa.CallCommon {
	args := make([]mxssa.Value, len(cc.Args))
	for j, a := range cc.Args {
		args[j] = t.value(a, valMap)
	}
	mcc := mxssa.CallCommon{
		Value:    t.value(cc.Value, valMap),
		Args:     args,
		IsInvoke: cc.IsInvoke(),
	}
	if cc.Method != nil {
		if fn, ok := t.typ(cc.Method.Type()).(*typecheck.Signature); ok {
			mcc.Method = typecheck.NewFunc(nil, cc.Method.Name(), fn)
		}
	}
	mcc.SetPos(cc.Pos())
	return mcc
}
