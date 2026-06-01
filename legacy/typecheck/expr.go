package typecheck

import "moxie/syntax"

// checkExpr type-checks an expression and returns its type.
// It also records the type info in c.info if non-nil.
func (c *Checker) checkExpr(e syntax.Expr, scope *Scope) Type {
	if e == nil {
		return nil
	}
	tv := c.typeExpr(e, scope)
	c.record(e, tv)
	return tv.Type
}

// typeExpr computes the TypeAndValue for expression e.
func (c *Checker) typeExpr(e syntax.Expr, scope *Scope) TypeAndValue {
	switch e := e.(type) {
	case *syntax.Name:
		return c.typeIdent(e, scope)
	case *syntax.BasicLit:
		return c.typeBasicLit(e)
	case *syntax.Operation:
		return c.typeOperation(e, scope)
	case *syntax.CallExpr:
		return c.typeCall(e, scope)
	case *syntax.SelectorExpr:
		return c.typeSelector(e, scope)
	case *syntax.IndexExpr:
		return c.typeIndex(e, scope)
	case *syntax.SliceExpr:
		return c.typeSlice(e, scope)
	case *syntax.AssertExpr:
		return c.typeAssert(e, scope)
	case *syntax.TypeSwitchGuard:
		return TypeAndValue{Type: c.checkExpr(e.X, scope), mode: modeValue}
	case *syntax.CompositeLit:
		return c.typeCompositeLit(e, scope)
	case *syntax.FuncLit:
		return c.typeFuncLit(e, scope)
	case *syntax.KeyValueExpr:
		// key:value — type is the value's type
		c.checkExpr(e.Key, scope)
		return c.typeExpr(e.Value, scope)
	case *syntax.ParenExpr:
		return c.typeExpr(e.X, scope)
	case *syntax.ListExpr:
		// multi-value list (tuple unpacking context)
		var last TypeAndValue
		for _, el := range e.ElemList {
			last = c.typeExpr(el, scope)
		}
		return last
	// type expressions used where a value is expected
	case *syntax.SliceType, *syntax.ArrayType, *syntax.MapType,
		*syntax.ChanType, *syntax.StructType, *syntax.InterfaceType,
		*syntax.FuncType, *syntax.DotsType:
		typ := c.resolveTypeExpr(e)
		return TypeAndValue{Type: typ, mode: modeType}
	}
	return TypeAndValue{}
}

func (c *Checker) typeIdent(e *syntax.Name, scope *Scope) TypeAndValue {
	if e.Value == "_" {
		return TypeAndValue{mode: modeValue}
	}
	_, obj := c.lookup(e.Value, scope)
	if obj == nil {
		c.errorf(e.Pos(), "undefined: %s", e.Value)
		return TypeAndValue{}
	}
	if c.info != nil {
		c.info.Uses[e] = obj
	}
	switch obj := obj.(type) {
	case *Var:
		return TypeAndValue{Type: obj.typ, mode: modeVar}
	case *Const:
		return TypeAndValue{Type: obj.typ, Value: obj.val, mode: modeConst}
	case *TypeName:
		return TypeAndValue{Type: obj.typ, mode: modeType}
	case *Func:
		return TypeAndValue{Type: obj.typ, mode: modeValue}
	case *Builtin:
		return TypeAndValue{mode: modeBuiltin}
	case *PkgName:
		return TypeAndValue{mode: modePkg}
	}
	return TypeAndValue{}
}

func (c *Checker) typeBasicLit(e *syntax.BasicLit) TypeAndValue {
	switch e.Kind {
	case syntax.IntLit:
		return TypeAndValue{Type: Typ[UntypedInt], mode: modeConst}
	case syntax.FloatLit:
		return TypeAndValue{Type: Typ[UntypedFloat], mode: modeConst}
	case syntax.StringLit:
		return TypeAndValue{Type: Typ[UntypedString], mode: modeConst}
	case syntax.RuneLit:
		return TypeAndValue{Type: Typ[UntypedRune], mode: modeConst}
	}
	return TypeAndValue{}
}

func (c *Checker) typeOperation(e *syntax.Operation, scope *Scope) TypeAndValue {
	if e.Y == nil {
		// unary
		xt := c.checkExpr(e.X, scope)
		if xt == nil {
			return TypeAndValue{mode: modeValue}
		}
		switch e.Op {
		case syntax.Recv: // <-ch
			if ch, ok := xt.Underlying().(*Chan); ok {
				return TypeAndValue{Type: ch.elem, mode: modeValue}
			}
		case syntax.And: // &x
			return TypeAndValue{Type: NewPointer(xt), mode: modeValue}
		case syntax.Mul: // *x (dereference)
			if pt, ok := xt.Underlying().(*Pointer); ok {
				return TypeAndValue{Type: pt.base, mode: modeVar}
			}
		default:
			return TypeAndValue{Type: xt, mode: modeValue}
		}
		return TypeAndValue{mode: modeValue}
	}
	// binary
	xt := c.checkExpr(e.X, scope)
	yt := c.checkExpr(e.Y, scope)
	switch e.Op {
	case syntax.Eql, syntax.Neq, syntax.Lss, syntax.Leq, syntax.Gtr, syntax.Geq,
		syntax.AndAnd, syntax.OrOr:
		return TypeAndValue{Type: Typ[Bool], mode: modeValue}
	case syntax.Or, syntax.Add:
		// In Moxie, | on strings is concat. Use dominant side's type.
		if xt != nil {
			return TypeAndValue{Type: xt, mode: modeValue}
		}
		return TypeAndValue{Type: yt, mode: modeValue}
	default:
		if xt != nil {
			return TypeAndValue{Type: xt, mode: modeValue}
		}
		return TypeAndValue{Type: yt, mode: modeValue}
	}
}

func (c *Checker) typeCall(e *syntax.CallExpr, scope *Scope) TypeAndValue {
	funTV := c.typeExpr(e.Fun, scope)
	if c.info != nil {
		c.info.Types[e.Fun] = funTV
	}

	for _, arg := range e.ArgList {
		c.checkExpr(arg, scope)
	}

	if funTV.mode == modeBuiltin {
		return c.typeBuiltinCall(e, scope)
	}
	if funTV.mode == modeType {
		// conversion: T(x)
		if len(e.ArgList) == 1 {
			c.checkExpr(e.ArgList[0], scope)
		}
		return TypeAndValue{Type: funTV.Type, mode: modeValue}
	}

	if funTV.Type == nil {
		return TypeAndValue{}
	}
	sig, ok := funTV.Type.Underlying().(*Signature)
	if !ok {
		return TypeAndValue{}
	}
	if sig.results == nil || sig.results.Len() == 0 {
		return TypeAndValue{mode: modeVoid}
	}
	if sig.results.Len() == 1 {
		return TypeAndValue{Type: sig.results.At(0).typ, mode: modeValue}
	}
	// multi-return: return a Tuple type
	return TypeAndValue{Type: sig.results, mode: modeValue}
}

func (c *Checker) typeBuiltinCall(e *syntax.CallExpr, scope *Scope) TypeAndValue {
	// Determine which builtin from the ident.
	name, ok := e.Fun.(*syntax.Name)
	if !ok {
		return TypeAndValue{}
	}
	_, obj := c.lookup(name.Value, scope)
	b, ok := obj.(*Builtin)
	if !ok {
		return TypeAndValue{}
	}
	switch b.id {
	case BuiltinLen, BuiltinCap:
		return TypeAndValue{Type: Typ[Int32], mode: modeValue}
	case BuiltinAppend:
		if len(e.ArgList) > 0 {
			return TypeAndValue{Type: c.checkExpr(e.ArgList[0], scope), mode: modeValue}
		}
	case BuiltinMake:
		if len(e.ArgList) > 0 {
			return TypeAndValue{Type: c.resolveTypeExpr(e.ArgList[0]), mode: modeValue}
		}
	case BuiltinNew:
		if len(e.ArgList) > 0 {
			return TypeAndValue{Type: NewPointer(c.resolveTypeExpr(e.ArgList[0])), mode: modeValue}
		}
	case BuiltinPanic, BuiltinPrint, BuiltinPrintln:
		return TypeAndValue{mode: modeVoid}
	case BuiltinRecover:
		return TypeAndValue{Type: universeError.typ, mode: modeValue}
	case BuiltinClose, BuiltinDelete, BuiltinClear:
		return TypeAndValue{mode: modeVoid}
	case BuiltinCopy:
		return TypeAndValue{Type: Typ[Int32], mode: modeValue}
	case BuiltinSpawn:
		return TypeAndValue{mode: modeVoid}
	case BuiltinFree:
		return TypeAndValue{mode: modeVoid}
	}
	return TypeAndValue{}
}

func (c *Checker) typeSelector(e *syntax.SelectorExpr, scope *Scope) TypeAndValue {
	xt := c.typeExpr(e.X, scope)
	if c.info != nil {
		c.info.Types[e.X] = xt
	}
	if xt.mode == modePkg {
		// package.Name
		if pkgName, ok := e.X.(*syntax.Name); ok {
			_, pkgObj := c.lookup(pkgName.Value, scope)
			if pn, ok := pkgObj.(*PkgName); ok && pn.imported != nil {
				obj := pn.imported.scope.Lookup(e.Sel.Value)
				if obj != nil {
					if c.info != nil {
						c.info.Uses[e.Sel] = obj
					}
					return TypeAndValue{Type: obj.Type(), mode: modeValue}
				}
			}
		}
		return TypeAndValue{}
	}

	// field or method lookup
	typ := xt.Type
	if typ == nil {
		return TypeAndValue{}
	}
	sel := c.lookupFieldOrMethod(typ, e.Sel.Value)
	if sel != nil {
		if c.info != nil {
			c.info.Selections[e] = sel
			if sel.obj != nil {
				c.info.Uses[e.Sel] = sel.obj
			}
		}
		return TypeAndValue{Type: sel.Type(), mode: modeValue}
	}
	return TypeAndValue{}
}

func (c *Checker) typeIndex(e *syntax.IndexExpr, scope *Scope) TypeAndValue {
	xt := c.checkExpr(e.X, scope)
	c.checkExpr(e.Index, scope)
	if xt == nil {
		return TypeAndValue{}
	}
	switch t := xt.Underlying().(type) {
	case *Array:
		return TypeAndValue{Type: t.elem, mode: modeVar}
	case *Slice:
		return TypeAndValue{Type: t.elem, mode: modeVar}
	case *Map:
		return TypeAndValue{Type: t.elem, mode: modeValue}
	}
	return TypeAndValue{}
}

func (c *Checker) typeSlice(e *syntax.SliceExpr, scope *Scope) TypeAndValue {
	xt := c.checkExpr(e.X, scope)
	for _, idx := range e.Index {
		if idx != nil {
			c.checkExpr(idx, scope)
		}
	}
	if xt == nil {
		return TypeAndValue{}
	}
	switch t := xt.Underlying().(type) {
	case *Array:
		return TypeAndValue{Type: NewSlice(t.elem), mode: modeValue}
	case *Slice:
		return TypeAndValue{Type: xt, mode: modeValue}
	}
	// string[:] → string
	if b, ok := xt.Underlying().(*Basic); ok && b.info&IsString != 0 {
		return TypeAndValue{Type: xt, mode: modeValue}
	}
	return TypeAndValue{}
}

func (c *Checker) typeAssert(e *syntax.AssertExpr, scope *Scope) TypeAndValue {
	c.checkExpr(e.X, scope)
	assertedType := c.resolveTypeExpr(e.Type)
	return TypeAndValue{Type: assertedType, mode: modeValue}
}

func (c *Checker) typeCompositeLit(e *syntax.CompositeLit, scope *Scope) TypeAndValue {
	var typ Type
	if e.Type != nil {
		typ = c.resolveTypeExpr(e.Type)
	}
	// Determine whether keys in key:value pairs are field names (struct) or
	// expressions (map/slice). Struct field names are not in scope.
	// Also treat typeless composite literals (e.Type == nil) as structs —
	// in Go they appear as slice/array element literals where type is inferred.
	isStruct := e.Type == nil || (typ != nil && isStructType(typ))
	// Fallback: if we have key:value elements but couldn't determine the type,
	// assume struct (struct field names don't exist as scope variables).
	if !isStruct && len(e.ElemList) > 0 {
		if _, ok := e.ElemList[0].(*syntax.KeyValueExpr); ok {
			isStruct = true
		}
	}
	for _, el := range e.ElemList {
		if kv, ok := el.(*syntax.KeyValueExpr); ok {
			if isStruct {
				// Struct field: only check value, skip key lookup.
				c.checkExpr(kv.Value, scope)
			} else {
				// Map/unknown: check both key and value.
				c.checkExpr(kv.Key, scope)
				c.checkExpr(kv.Value, scope)
			}
		} else {
			c.checkExpr(el, scope)
		}
	}
	return TypeAndValue{Type: typ, mode: modeValue}
}

// isStructType returns true if t is (or is a Named wrapping) a struct.
// Named types with nil underlying (unresolved) are treated as structs
// because composite literals with key:value syntax on a Named type are
// always struct literals in Moxie code — map literals use an explicit
// map[K]V type, never a Named alias at this level.
func isStructType(t Type) bool {
	if t == nil {
		return false
	}
	switch t := t.(type) {
	case *Struct:
		return true
	case *Named:
		if t.underlying == nil {
			return true // optimistic: Named with key:value syntax = struct
		}
		return isStructType(t.underlying)
	}
	return false
}

func (c *Checker) typeFuncLit(e *syntax.FuncLit, scope *Scope) TypeAndValue {
	sig := c.resolveFuncType(e.Type, nil)
	inner := c.openScope(e.Body, scope)
	if sig != nil {
		if sig.params != nil {
			for i := 0; i < sig.params.Len(); i++ {
				p := sig.params.At(i)
				if p.name != "" {
					inner.Insert(p)
				}
			}
		}
		if sig.results != nil {
			for i := 0; i < sig.results.Len(); i++ {
				r := sig.results.At(i)
				if r.name != "" {
					inner.Insert(r)
				}
			}
		}
	}
	c.checkBlock(e.Body, inner)
	return TypeAndValue{Type: sig, mode: modeValue}
}

// lookupFieldOrMethod finds a field or method named name on type t.
func (c *Checker) lookupFieldOrMethod(t Type, name string) *Selection {
	if t == nil {
		return nil
	}
	// dereference pointer
	indirect := false
	if pt, ok := safeUnderlying(t).(*Pointer); ok {
		t = pt.base
		indirect = true
	}

	switch t := safeUnderlying(t).(type) {
	case *Struct:
		for i, f := range t.fields {
			if f.name == name {
				return &Selection{
					kind:  FieldVal,
					recv:  t,
					obj:   f,
					index: []int{i},
					indir: indirect,
				}
			}
		}
	case *Interface:
		for _, m := range t.allMethods {
			if m.name == name {
				fn := NewFunc(nil, name, m.sig)
				return &Selection{kind: MethodVal, recv: t, obj: fn, indir: indirect}
			}
		}
	case *Named:
		for _, m := range t.methods {
			if m.name == name {
				return &Selection{kind: MethodVal, recv: t, obj: m, indir: indirect}
			}
		}
		if t.underlying != nil {
			return c.lookupFieldOrMethod(t.underlying, name)
		}
	}
	return nil
}
