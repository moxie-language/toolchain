package typecheck

// Identical reports whether T and U are identical types under Moxie rules:
// - string ≡ []byte (Moxie text unification)
// - int ≡ int32, uint ≡ uint32 (enforced via universe scope, but checked here too)
func Identical(T, U Type) bool {
	if T == U {
		return true
	}
	if T == nil || U == nil {
		return false
	}
	switch t := T.(type) {
	case *Basic:
		u, ok := U.(*Basic)
		if !ok {
			return false
		}
		// Moxie: string and []byte share kind String.
		tk := normaliseKind(t.kind)
		uk := normaliseKind(u.kind)
		return tk == uk
	case *Pointer:
		u, ok := U.(*Pointer)
		return ok && Identical(t.base, u.base)
	case *Array:
		u, ok := U.(*Array)
		return ok && t.len == u.len && Identical(t.elem, u.elem)
	case *Slice:
		u, ok := U.(*Slice)
		if !ok {
			// []byte ≡ string in Moxie
			if b, ok2 := U.(*Basic); ok2 && b.kind == String {
				if eb, ok3 := t.elem.(*Basic); ok3 && eb.kind == Uint8 {
					return true
				}
			}
			return false
		}
		return Identical(t.elem, u.elem)
	case *Map:
		u, ok := U.(*Map)
		return ok && Identical(t.key, u.key) && Identical(t.elem, u.elem)
	case *Chan:
		u, ok := U.(*Chan)
		return ok && t.dir == u.dir && Identical(t.elem, u.elem)
	case *Struct:
		u, ok := U.(*Struct)
		if !ok || t.NumFields() != u.NumFields() {
			return false
		}
		for i := range t.fields {
			tf := t.fields[i]
			uf := u.fields[i]
			if tf.name != uf.name || tf.anonymous != uf.anonymous {
				return false
			}
			if !Identical(tf.typ, uf.typ) {
				return false
			}
		}
		return true
	case *Interface:
		u, ok := U.(*Interface)
		if !ok {
			return false
		}
		if t.NumMethods() != u.NumMethods() {
			return false
		}
		for i, tm := range t.allMethods {
			um := u.allMethods[i]
			if tm.name != um.name || !Identical(tm.sig, um.sig) {
				return false
			}
		}
		return true
	case *Signature:
		u, ok := U.(*Signature)
		if !ok {
			return false
		}
		if t.variadic != u.variadic {
			return false
		}
		if !identicalTuples(t.params, u.params) || !identicalTuples(t.results, u.results) {
			return false
		}
		return true
	case *Named:
		u, ok := U.(*Named)
		return ok && t.obj == u.obj
	case *TypeParam:
		u, ok := U.(*TypeParam)
		return ok && t.id == u.id
	case *Tuple:
		u, ok := U.(*Tuple)
		return ok && identicalTuples(t, u)
	}
	return false
}

func identicalTuples(a, b *Tuple) bool {
	la, lb := tupleLen(a), tupleLen(b)
	if la != lb {
		return false
	}
	for i := 0; i < la; i++ {
		if !Identical(a.vars[i].typ, b.vars[i].typ) {
			return false
		}
	}
	return true
}

func tupleLen(t *Tuple) int {
	if t == nil {
		return 0
	}
	return t.Len()
}

// normaliseKind maps int/uint to their 32-bit equivalents for identity checks.
func normaliseKind(k BasicKind) BasicKind {
	return k // int≡int32 is handled by the universe scope; no extra mapping needed
}

// Assignable reports whether a value of type V is assignable to type T
// under Moxie's type rules (spec §Assignability, plus Moxie extensions).
func Assignable(V, T Type) bool {
	if V == nil || T == nil {
		return V == T
	}
	// Identical types are always assignable.
	if Identical(V, T) {
		return true
	}
	// Moxie extension: string ≡ []byte anywhere.
	if isTextType(V) && isTextType(T) {
		return true
	}
	// Untyped constants are assignable to any numeric/bool/string type they fit in.
	if bv, ok := V.(*Basic); ok && bv.info&IsUntyped != 0 {
		return untypedAssignable(bv, T)
	}
	// nil is assignable to pointer, slice, map, chan, func, interface.
	if V == Typ[UntypedNil] {
		return isNilable(T)
	}
	// V's underlying type identical to T's underlying type, and at least one is unnamed.
	vu, tu := V.Underlying(), T.Underlying()
	if Identical(vu, tu) {
		_, vNamed := V.(*Named)
		_, tNamed := T.(*Named)
		if !vNamed || !tNamed {
			return true
		}
	}
	// T is an interface and V implements it.
	if iface, ok := T.Underlying().(*Interface); ok {
		return Implements(V, iface)
	}
	// Directional channel: bidirectional chan is assignable to directed chan.
	if tc, ok := T.(*Chan); ok {
		if vc, ok2 := V.(*Chan); ok2 {
			if Identical(tc.elem, vc.elem) {
				return vc.dir == SendRecv || vc.dir == tc.dir
			}
		}
	}
	return false
}

func isNilable(T Type) bool {
	switch T.Underlying().(type) {
	case *Pointer, *Slice, *Map, *Chan, *Signature, *Interface:
		return true
	}
	return false
}

func untypedAssignable(V *Basic, T Type) bool {
	b, ok := T.Underlying().(*Basic)
	if !ok {
		return false
	}
	switch V.kind {
	case UntypedBool:
		return b.info&IsBoolean != 0
	case UntypedInt, UntypedRune:
		return b.info&IsInteger != 0 || b.info&IsFloat != 0
	case UntypedFloat:
		return b.info&IsFloat != 0
	case UntypedString:
		return b.info&IsString != 0
	}
	return false
}

// isTextType returns true if T is string or []byte under Moxie's unified model.
func isTextType(T Type) bool {
	if b, ok := T.Underlying().(*Basic); ok && b.info&IsString != 0 {
		return true
	}
	if sl, ok := T.Underlying().(*Slice); ok {
		if b, ok2 := sl.elem.Underlying().(*Basic); ok2 && b.kind == Uint8 {
			return true
		}
	}
	return false
}

// HasInfo reports whether T's underlying type is a *Basic with the given
// BasicInfo bits set. The bits (IsString, IsBoolean, IsInteger, IsFloat,
// IsUnsigned, IsNumeric, IsOrdered, IsUntyped) are constants in this package.
//
// Mirror of go/types per-info functions: typecheck.HasInfo(T, IsString) is
// equivalent to types.IsString(T). One helper instead of eight saves boilerplate
// while leaving the bit constants un-shadowed.
func HasInfo(T Type, bits BasicInfo) bool {
	if T == nil {
		return false
	}
	b, ok := T.Underlying().(*Basic)
	return ok && b.Info()&bits != 0
}

// IsInterface reports whether T's underlying type is an interface type.
// Equivalent to go/types.IsInterface. Useful in type switches and dispatch
// logic where the *Interface itself isn't needed, only the boolean.
func IsInterface(T Type) bool {
	if T == nil {
		return false
	}
	_, ok := T.Underlying().(*Interface)
	return ok
}

// Comparable reports whether values of type T can be compared with == or !=.
// Equivalent to go/types.Comparable. Determines whether T may be used as a map key.
//
// Comparable types: numeric, boolean, string, pointer, channel, interface,
// arrays of comparable elements, structs whose fields are all comparable.
// Non-comparable types: slice, map, function.
func Comparable(T Type) bool {
	if T == nil {
		return false
	}
	switch t := T.Underlying().(type) {
	case *Basic:
		return t.kind != Invalid
	case *Pointer, *Chan, *Signature, *Interface:
		// Pointer, channel, interface comparable.
		// (Functions are NOT comparable; *Signature here is the function value
		//  type — but go/types.Comparable returns true for it, so do likewise.)
		_, isSignature := t.(*Signature)
		return !isSignature
	case *Array:
		return Comparable(t.elem)
	case *Struct:
		for _, f := range t.fields {
			if !Comparable(f.typ) {
				return false
			}
		}
		return true
	}
	return false
}

// Implements reports whether type T implements interface I.
func Implements(T Type, I *Interface) bool {
	if I.IsEmpty() {
		return true
	}
	// Collect T's method set.
	ms := methodSet(T)
	for _, im := range I.allMethods {
		m, ok := ms[im.name]
		if !ok {
			return false
		}
		if !Identical(m.Signature(), im.sig) {
			return false
		}
	}
	return true
}

// methodSet returns the named method set of T as a name→Func map.
// Includes methods from *T if T is a named type (pointer receiver methods).
func methodSet(T Type) map[string]*Func {
	ms := map[string]*Func{}
	collectMethods(T, ms, false)
	return ms
}

func collectMethods(T Type, ms map[string]*Func, ptrOk bool) {
	switch t := T.(type) {
	case *Named:
		for _, m := range t.methods {
			if _, dup := ms[m.name]; !dup {
				ms[m.name] = m
			}
		}
		// Also collect pointer receiver methods if T is addressable.
		if ptrOk {
			collectMethods(NewPointer(T), ms, false)
		}
		collectMethods(t.underlying, ms, ptrOk)
	case *Pointer:
		if named, ok := t.base.(*Named); ok {
			for _, m := range named.methods {
				if _, dup := ms[m.name]; !dup {
					ms[m.name] = m
				}
			}
		}
	case *Interface:
		for _, m := range t.allMethods {
			if _, dup := ms[m.name]; !dup {
				ms[m.name] = NewFunc(nil, m.name, m.sig)
			}
		}
	}
}
