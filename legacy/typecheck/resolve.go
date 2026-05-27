package typecheck

import "moxie/syntax"

// resolveTypeExpr resolves a type expression to a Type.
// This is the core of the type system — translating AST type nodes to Type values.
func (c *Checker) resolveTypeExpr(e syntax.Expr) Type {
	if e == nil {
		return nil
	}
	switch e := e.(type) {
	case *syntax.Name:
		return c.resolveTypeName(e)
	case *syntax.SelectorExpr:
		return c.resolveQualifiedTypeName(e)
	case *syntax.Operation:
		// *T (pointer)
		if e.Y == nil && e.Op == syntax.Mul {
			base := c.resolveTypeExpr(e.X)
			if base == nil {
				return nil
			}
			return NewPointer(base)
		}
		// ~T (approximation element in type constraints) — treat as T for B1
		if e.Y == nil && e.Op == syntax.Tilde {
			return c.resolveTypeExpr(e.X)
		}
		// T | U (union element in type constraints) — return first operand for B1
		if e.Y != nil && e.Op == syntax.Or {
			c.resolveTypeExpr(e.Y) // resolve both sides to catch undefined errors
			return c.resolveTypeExpr(e.X)
		}
	case *syntax.SliceType:
		elem := c.resolveTypeExpr(e.Elem)
		if elem == nil {
			return nil
		}
		return NewSlice(elem)
	case *syntax.ArrayType:
		elem := c.resolveTypeExpr(e.Elem)
		if elem == nil {
			return nil
		}
		if e.Len == nil {
			// [...]T — length inferred from composite literal
			return NewArray(elem, -1)
		}
		n := c.evalArrayLen(e.Len)
		return NewArray(elem, n)
	case *syntax.MapType:
		key := c.resolveTypeExpr(e.Key)
		val := c.resolveTypeExpr(e.Value)
		if key == nil || val == nil {
			return nil
		}
		return NewMap(key, val)
	case *syntax.ChanType:
		elem := c.resolveTypeExpr(e.Elem)
		if elem == nil {
			return nil
		}
		var dir ChanDir
		switch e.Dir {
		case syntax.SendOnly:
			dir = SendOnly
		case syntax.RecvOnly:
			dir = RecvOnly
		default:
			dir = SendRecv
		}
		return NewChan(dir, elem)
	case *syntax.StructType:
		return c.resolveStructType(e)
	case *syntax.InterfaceType:
		return c.resolveInterfaceType(e)
	case *syntax.FuncType:
		return c.resolveFuncType(e, nil)
	case *syntax.IndexExpr:
		// generic instantiation T[A, B, ...]
		return c.resolveGenericInst(e)
	case *syntax.DotsType:
		// variadic element type (used in parameter lists, not standalone)
		return c.resolveTypeExpr(e.Elem)
	}
	c.errorf(e.Pos(), "cannot resolve type expression")
	return nil
}

func (c *Checker) resolveTypeName(e *syntax.Name) Type {
	_, obj := c.lookupType(e.Value)
	if obj == nil {
		c.errorf(e.Pos(), "undefined: %s", e.Value)
		return nil
	}
	if tn, ok := obj.(*TypeName); ok {
		if c.info != nil {
			c.info.Uses[e] = tn
		}
		return tn.typ
	}
	c.errorf(e.Pos(), "%s is not a type", e.Value)
	return nil
}

func (c *Checker) resolveQualifiedTypeName(e *syntax.SelectorExpr) Type {
	pkgName, ok := e.X.(*syntax.Name)
	if !ok {
		c.errorf(e.Pos(), "invalid type expression")
		return nil
	}
	_, obj := c.lookup(pkgName.Value, c.pkg.scope)
	if obj == nil {
		c.errorf(e.Pos(), "undefined: %s", pkgName.Value)
		return nil
	}
	pkgObj, ok := obj.(*PkgName)
	if !ok {
		c.errorf(e.Pos(), "%s is not a package", pkgName.Value)
		return nil
	}
	imported := pkgObj.imported
	if imported == nil {
		return nil
	}
	typeObj := imported.scope.Lookup(e.Sel.Value)
	if typeObj == nil {
		c.errorf(e.Sel.Pos(), "undefined: %s.%s", pkgName.Value, e.Sel.Value)
		return nil
	}
	if tn, ok := typeObj.(*TypeName); ok {
		return tn.typ
	}
	c.errorf(e.Sel.Pos(), "%s.%s is not a type", pkgName.Value, e.Sel.Value)
	return nil
}

func (c *Checker) resolveStructType(e *syntax.StructType) *Struct {
	var fields []*Var
	var tags []string
	for i, f := range e.FieldList {
		typ := c.resolveTypeExpr(f.Type)
		if f.Name != nil {
			fields = append(fields, NewField(c.pkg, f.Name.Value, typ, false))
		} else {
			// anonymous/embedded
			name := typeBaseName(typ)
			fields = append(fields, NewField(c.pkg, name, typ, true))
		}
		tag := ""
		if i < len(e.TagList) && e.TagList[i] != nil {
			tag = e.TagList[i].Value
		}
		tags = append(tags, tag)
	}
	return NewStruct(fields, tags)
}

func (c *Checker) resolveInterfaceType(e *syntax.InterfaceType) *Interface {
	var methods []*IfaceMethod
	var embeds []Type
	for _, f := range e.MethodList {
		if f.Name == nil {
			// embedded type
			typ := c.resolveTypeExpr(f.Type)
			if typ != nil {
				embeds = append(embeds, typ)
			}
			continue
		}
		ft, ok := f.Type.(*syntax.FuncType)
		if !ok {
			continue
		}
		sig := c.resolveFuncType(ft, nil)
		if sig != nil {
			methods = append(methods, &IfaceMethod{name: f.Name.Value, sig: sig})
		}
	}
	iface := NewInterface(methods, embeds)
	iface.Complete()
	return iface
}

// resolveFuncType converts a FuncType AST node to a Signature.
// recv is optional (non-nil only for method declarations).
func (c *Checker) resolveFuncType(ft *syntax.FuncType, recv *syntax.Field) *Signature {
	if ft == nil {
		return nil
	}
	var recvVar *Var
	if recv != nil {
		recvTyp := c.resolveTypeExpr(recv.Type)
		recvName := ""
		if recv.Name != nil {
			recvName = recv.Name.Value
		}
		recvVar = NewVar(c.pkg, recvName, recvTyp)
	}

	params := c.resolveFieldList(ft.ParamList)
	results := c.resolveFieldList(ft.ResultList)

	variadic := false
	if len(ft.ParamList) > 0 {
		last := ft.ParamList[len(ft.ParamList)-1]
		if _, ok := last.Type.(*syntax.DotsType); ok {
			variadic = true
			// unwrap: the slice type is already resolved in resolveFieldList
		}
	}

	return NewSignature(recvVar, params, results, variadic)
}

func (c *Checker) resolveFieldList(fields []*syntax.Field) *Tuple {
	if len(fields) == 0 {
		return nil
	}
	var vars []*Var
	for _, f := range fields {
		typ := c.resolveTypeExpr(f.Type)
		name := ""
		if f.Name != nil {
			name = f.Name.Value
		}
		vars = append(vars, NewVar(c.pkg, name, typ))
	}
	return NewTuple(vars...)
}

func (c *Checker) resolveGenericInst(e *syntax.IndexExpr) Type {
	// TODO: full generic instantiation
	return c.resolveTypeExpr(e.X)
}

// typeBaseName returns the name of the base type for an anonymous field.
func typeBaseName(t Type) string {
	if t == nil {
		return ""
	}
	switch t := t.(type) {
	case *Named:
		if t.obj != nil {
			return t.obj.name
		}
	case *Pointer:
		return typeBaseName(t.base)
	case *Basic:
		return t.name
	}
	return ""
}
