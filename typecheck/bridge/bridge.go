// Package bridge translates go/types objects into typecheck objects.
// It is a temporary shim used during B1 validation: the loader uses go/types
// to parse and type-check packages, and this bridge exposes the results to
// the native typecheck.Checker so it can look up imported names and types.
//
// Removed in B3 when the native checker replaces go/types entirely.
package bridge

import (
	"go/types"
	"sync"
	"unsafe"

	"moxie/typecheck"
)

// Importer wraps a go/types package map and satisfies typecheck.Importer.
// Each package is translated lazily and cached.
type Importer struct {
	mu        sync.Mutex
	pkgs      map[string]*types.Package // import path → go/types Package
	cache     map[string]*typecheck.Package
	typeCache *typeCache // global type cache for pointer identity across calls
}

// New returns an Importer pre-loaded with the given package map.
func New(pkgs map[string]*types.Package) *Importer {
	return &Importer{
		pkgs:      pkgs,
		cache:     map[string]*typecheck.Package{},
		typeCache: newTypeCache(),
	}
}

// Import satisfies typecheck.Importer.
func (imp *Importer) Import(path string) (*typecheck.Package, error) {
	imp.mu.Lock()
	defer imp.mu.Unlock()
	if p, ok := imp.cache[path]; ok {
		return p, nil
	}
	gp, ok := imp.pkgs[path]
	if !ok {
		// Return an empty package — the checker will report "undefined"
		// for any names not found, which is the correct B1 behaviour.
		p := typecheck.NewPackage(path, "")
		imp.cache[path] = p
		return p, nil
	}
	p := translatePackage(gp, imp)
	imp.cache[path] = p
	return p, nil
}

// typeCache maps go/types.Type values to their typecheck equivalents.
// Keyed by pointer identity (uintptr extracted from the interface data word)
// to avoid the runtime's typehash recursing into complex recursive types.
type typeCache struct {
	m     map[uintptr]typecheck.Type
	depth int // recursion depth limiter
}

func newTypeCache() *typeCache { return &typeCache{m: map[uintptr]typecheck.Type{}} }

// typePtr extracts the data pointer from a types.Type interface value.
// This gives stable pointer identity without triggering recursive hashing.
func typePtr(t types.Type) uintptr {
	type iface struct{ typ, ptr uintptr }
	return (*iface)(unsafe.Pointer(&t)).ptr
}

func translatePackage(gp *types.Package, imp *Importer) *typecheck.Package {
	p := typecheck.NewPackage(gp.Path(), gp.Name())
	cache := newTypeCache()
	scope := gp.Scope()
	for _, name := range scope.Names() {
		obj := scope.Lookup(name)
		if obj == nil {
			continue
		}
		tc := translateObject(obj, p, cache, imp)
		if tc != nil {
			p.Scope().Insert(tc)
		}
	}
	p.MarkComplete()
	return p
}

// TranslateType converts a go/types.Type to a typecheck.Type using imp's global cache.
// Using the importer's global typeCache ensures the same go/types.Type always
// maps to the same typecheck.Type pointer, keeping revTypes consistent.
func TranslateType(imp *Importer, t types.Type) typecheck.Type {
	return translateType(t, imp.typeCache, imp)
}

// TranslateObject converts a go/types.Object to a typecheck.Object.
func TranslateObject(imp *Importer, obj types.Object) typecheck.Object {
	return translateObject(obj, nil, newTypeCache(), imp)
}

func translateObject(obj types.Object, pkg *typecheck.Package, cache *typeCache, imp *Importer) typecheck.Object {
	typ := translateType(obj.Type(), cache, imp)
	switch obj := obj.(type) {
	case *types.TypeName:
		return typecheck.NewTypeName(pkg, obj.Name(), typ)
	case *types.Func:
		if sig, ok := typ.(*typecheck.Signature); ok {
			return typecheck.NewFunc(pkg, obj.Name(), sig)
		}
		return typecheck.NewFunc(pkg, obj.Name(), nil)
	case *types.Var:
		return typecheck.NewVar(pkg, obj.Name(), typ)
	case *types.Const:
		return typecheck.NewConst(pkg, obj.Name(), typ, obj.Val())
	case *types.PkgName:
		var imported *typecheck.Package
		if obj.Imported() != nil {
			imported, _ = imp.Import(obj.Imported().Path())
		}
		return typecheck.NewPkgName(pkg, obj.Name(), imported)
	}
	return nil
}

// translateType converts a go/types.Type to a typecheck.Type.
// The cache prevents infinite recursion on recursive types.
func translateType(t types.Type, cache *typeCache, imp *Importer) typecheck.Type {
	if t == nil {
		return nil
	}
	key := typePtr(t)
	if tc, ok := cache.m[key]; ok {
		return tc
	}
	// Depth limiter: skip translation for very deep type hierarchies.
	// The native checker will report "undefined" for anything that can't
	// be resolved, which is acceptable during B1 validation.
	cache.depth++
	defer func() { cache.depth-- }()
	if cache.depth > 150 {
		return nil
	}

	switch t := t.(type) {
	case *types.Basic:
		return translateBasic(t)

	case *types.Pointer:
		elem := translateType(t.Elem(), cache, imp)
		return typecheck.NewPointer(elem)

	case *types.Array:
		elem := translateType(t.Elem(), cache, imp)
		return typecheck.NewArray(elem, t.Len())

	case *types.Slice:
		elem := translateType(t.Elem(), cache, imp)
		return typecheck.NewSlice(elem)

	case *types.Map:
		key := translateType(t.Key(), cache, imp)
		elem := translateType(t.Elem(), cache, imp)
		return typecheck.NewMap(key, elem)

	case *types.Chan:
		elem := translateType(t.Elem(), cache, imp)
		var dir typecheck.ChanDir
		switch t.Dir() {
		case types.SendOnly:
			dir = typecheck.SendOnly
		case types.RecvOnly:
			dir = typecheck.RecvOnly
		default:
			dir = typecheck.SendRecv
		}
		return typecheck.NewChan(dir, elem)

	case *types.Struct:
		var fields []*typecheck.Var
		var tags []string
		for i := 0; i < t.NumFields(); i++ {
			f := t.Field(i)
			ft := translateType(f.Type(), cache, imp)
			fields = append(fields, typecheck.NewField(nil, f.Name(), ft, f.Anonymous()))
			tags = append(tags, t.Tag(i))
		}
		return typecheck.NewStruct(fields, tags)

	case *types.Interface:
		var methods []*typecheck.IfaceMethod
		for i := 0; i < t.NumExplicitMethods(); i++ {
			m := t.ExplicitMethod(i)
			sig, _ := translateType(m.Type(), cache, imp).(*typecheck.Signature)
			methods = append(methods, typecheck.NewIfaceMethod(m.Name(), sig))
		}
		var embeds []typecheck.Type
		for i := 0; i < t.NumEmbeddeds(); i++ {
			embeds = append(embeds, translateType(t.EmbeddedType(i), cache, imp))
		}
		iface := typecheck.NewInterface(methods, embeds)
		iface.Complete()
		return iface

	case *types.Signature:
		var recv *typecheck.Var
		if r := t.Recv(); r != nil {
			recv = typecheck.NewVar(nil, r.Name(), translateType(r.Type(), cache, imp))
		}
		params := translateTuple(t.Params(), cache, imp)
		results := translateTuple(t.Results(), cache, imp)
		return typecheck.NewSignature(recv, params, results, t.Variadic())

	case *types.Named:
		// Reserve a slot in the cache before recursing to handle cycles.
		obj := typecheck.NewTypeName(nil, t.Obj().Name(), nil)
		named := typecheck.NewNamed(obj, nil)
		cache.m[typePtr(t)] = named
		underlying := translateType(t.Underlying(), cache, imp)
		named.SetUnderlying(underlying)
		// Translate methods.
		for i := 0; i < t.NumMethods(); i++ {
			m := t.Method(i)
			sig, _ := translateType(m.Type(), cache, imp).(*typecheck.Signature)
			named.AddMethod(typecheck.NewFunc(nil, m.Name(), sig))
		}
		return named

	case *types.TypeParam:
		obj := typecheck.NewTypeName(nil, t.Obj().Name(), nil)
		return typecheck.NewTypeParam(obj, nil)

	case *types.Tuple:
		return translateTuple(t, cache, imp)

	case *types.Alias:
		return translateType(types.Unalias(t), cache, imp)
	}
	return nil
}

func translateTuple(t *types.Tuple, cache *typeCache, imp *Importer) *typecheck.Tuple {
	if t == nil || t.Len() == 0 {
		return nil
	}
	vars := make([]*typecheck.Var, t.Len())
	for i := range vars {
		v := t.At(i)
		vars[i] = typecheck.NewVar(nil, v.Name(), translateType(v.Type(), cache, imp))
	}
	return typecheck.NewTuple(vars...)
}

func translateBasic(t *types.Basic) *typecheck.Basic {
	switch t.Kind() {
	case types.Bool:
		return typecheck.Typ[typecheck.Bool]
	case types.Int8:
		return typecheck.Typ[typecheck.Int8]
	case types.Int16:
		return typecheck.Typ[typecheck.Int16]
	case types.Int32, types.Int: // Moxie: int≡int32
		return typecheck.Typ[typecheck.Int32]
	case types.Int64:
		return typecheck.Typ[typecheck.Int64]
	case types.Uint8:
		return typecheck.Typ[typecheck.Uint8]
	case types.Uint16:
		return typecheck.Typ[typecheck.Uint16]
	case types.Uint32, types.Uint: // Moxie: uint≡uint32
		return typecheck.Typ[typecheck.Uint32]
	case types.Uint64, types.Uintptr:
		return typecheck.Typ[typecheck.Uint64]
	case types.Float32:
		return typecheck.Typ[typecheck.Float32]
	case types.Float64:
		return typecheck.Typ[typecheck.Float64]
	case types.String:
		return typecheck.Typ[typecheck.String]
	case types.UnsafePointer:
		return typecheck.Typ[typecheck.UnsafePointer]
	case types.UntypedBool:
		return typecheck.Typ[typecheck.UntypedBool]
	case types.UntypedInt:
		return typecheck.Typ[typecheck.UntypedInt]
	case types.UntypedRune:
		return typecheck.Typ[typecheck.UntypedRune]
	case types.UntypedFloat:
		return typecheck.Typ[typecheck.UntypedFloat]
	case types.UntypedString:
		return typecheck.Typ[typecheck.UntypedString]
	case types.UntypedNil:
		return typecheck.Typ[typecheck.UntypedNil]
	}
	return typecheck.Typ[typecheck.Invalid]
}
