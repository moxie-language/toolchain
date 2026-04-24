package compiler

import (
	"fmt"
	"go/constant"
	"go/token"
	"os"

	"moxie/mxssa"
	"moxie/typecheck"
	tcbridge "moxie/typecheck/bridge"

	"golang.org/x/tools/go/ssa"
	"tinygo.org/x/go-llvm"
)

// getLLVMTypeTC returns an LLVM type for a typecheck.Type.
// Parallel to getLLVMType but operates on the native type system.
// Uses its own cache (tcLLVMTypes) since typeutil.Map only works with go/types.
func (c *compilerContext) getLLVMTypeTC(t typecheck.Type) llvm.Type {
	if cached, ok := c.tcLLVMTypes[t]; ok {
		return cached
	}
	lt := c.makeLLVMTypeTC(t)
	c.tcLLVMTypes[t] = lt
	return lt
}

func (c *compilerContext) makeLLVMTypeTC(t typecheck.Type) llvm.Type {
	switch typ := t.Underlying().(type) {
	case *typecheck.Basic:
		switch typ.Kind() {
		case typecheck.Bool, typecheck.UntypedBool:
			return c.ctx.Int1Type()
		case typecheck.Int8, typecheck.Uint8:
			return c.ctx.Int8Type()
		case typecheck.Int16, typecheck.Uint16:
			return c.ctx.Int16Type()
		case typecheck.Int32, typecheck.Uint32:
			return c.intType
		case typecheck.Int64, typecheck.Uint64:
			return c.ctx.Int64Type()
		case typecheck.Float32:
			return c.ctx.FloatType()
		case typecheck.Float64:
			return c.ctx.DoubleType()
		case typecheck.String, typecheck.UntypedString:
			return c.getLLVMRuntimeType("_string")
		case typecheck.UnsafePointer:
			return c.dataPtrType
		default:
			panic("tccompat: unknown basic kind: " + typ.String())
		}
	case *typecheck.Array:
		return llvm.ArrayType(c.getLLVMTypeTC(typ.Elem()), int(typ.Len()))
	case *typecheck.Slice:
		// Moxie: []byte uses the named _string type (string=[]byte unification).
		if basic, ok := typ.Elem().(*typecheck.Basic); ok && basic.Kind() == typecheck.Uint8 {
			return c.getLLVMRuntimeType("_string")
		}
		return c.ctx.StructType([]llvm.Type{
			c.dataPtrType,
			c.uintptrType,
			c.uintptrType,
		}, false)
	case *typecheck.Pointer, *typecheck.Map, *typecheck.Chan:
		return c.dataPtrType
	case *typecheck.Interface:
		return c.getLLVMRuntimeType("_interface")
	case *typecheck.Signature:
		return c.getFuncTypeTC(typ)
	case *typecheck.Struct:
		members := make([]llvm.Type, typ.NumFields())
		for i := range members {
			members[i] = c.getLLVMTypeTC(typ.Field(i).Type())
		}
		return c.ctx.StructType(members, false)
	case *typecheck.Tuple:
		members := make([]llvm.Type, typ.Len())
		for i := range members {
			members[i] = c.getLLVMTypeTC(typ.At(i).Type())
		}
		return c.ctx.StructType(members, false)
	case *typecheck.TypeParam:
		return c.getLLVMTypeTC(typ.Underlying())
	case *typecheck.Named:
		if st, ok := typ.Underlying().(*typecheck.Struct); ok {
			llvmName := typ.String()
			llvmType := c.ctx.StructCreateNamed(llvmName)
			c.tcLLVMTypes[t] = llvmType
			underlying := c.getLLVMTypeTC(st)
			llvmType.StructSetBody(underlying.StructElementTypes(), false)
			return llvmType
		}
		return c.getLLVMTypeTC(typ.Underlying())
	default:
		panic("tccompat: unknown type: " + t.String())
	}
}

// getFuncTypeTC returns the LLVM representation of a function value (closure pair).
func (c *compilerContext) getFuncTypeTC(typ *typecheck.Signature) llvm.Type {
	return c.ctx.StructType([]llvm.Type{c.dataPtrType, c.funcPtrType}, false)
}

// getLLVMFunctionTypeTC returns an LLVM function type for a typecheck.Signature.
func (c *compilerContext) getLLVMFunctionTypeTC(typ *typecheck.Signature) llvm.Type {
	var returnType llvm.Type
	results := typ.Results()
	switch {
	case results == nil || results.Len() == 0:
		returnType = c.ctx.VoidType()
	case results.Len() == 1:
		returnType = c.getLLVMTypeTC(results.At(0).Type())
	default:
		members := make([]llvm.Type, results.Len())
		for i := range members {
			members[i] = c.getLLVMTypeTC(results.At(i).Type())
		}
		returnType = c.ctx.StructType(members, false)
	}

	var paramTypes []llvm.Type
	if recv := typ.Recv(); recv != nil {
		paramTypes = append(paramTypes, c.getLLVMTypeTC(recv.Type()))
	}
	params := typ.Params()
	if params != nil {
		for i := 0; i < params.Len(); i++ {
			paramTypes = append(paramTypes, c.getLLVMTypeTC(params.At(i).Type()))
		}
	}

	return llvm.FunctionType(returnType, paramTypes, typ.Variadic())
}

// isPointerTC checks whether a typecheck.Type is a pointer type.
func isPointerTC(t typecheck.Type) bool {
	switch typ := t.Underlying().(type) {
	case *typecheck.Pointer:
		return true
	case *typecheck.Basic:
		return typ.Kind() == typecheck.UnsafePointer
	}
	return false
}

// --- Size computation for typecheck.Type (parallel to sizes.go) ---

var tcBasicSizes = [...]byte{
	typecheck.Bool:    1,
	typecheck.Int8:    1,
	typecheck.Int16:   2,
	typecheck.Int32:   4,
	typecheck.Int64:   8,
	typecheck.Uint8:   1,
	typecheck.Uint16:  2,
	typecheck.Uint32:  4,
	typecheck.Uint64:  8,
	typecheck.Float32: 4,
	typecheck.Float64: 8,
}

func (s *stdSizes) AlignofTC(T typecheck.Type) int64 {
	switch t := T.Underlying().(type) {
	case *typecheck.Array:
		return s.AlignofTC(t.Elem())
	case *typecheck.Struct:
		max := int64(1)
		for i := 0; i < t.NumFields(); i++ {
			if a := s.AlignofTC(t.Field(i).Type()); a > max {
				max = a
			}
		}
		return max
	case *typecheck.Slice, *typecheck.Interface:
		return s.PtrSize
	case *typecheck.Basic:
		if t.Info()&typecheck.IsString != 0 {
			return s.PtrSize
		}
	case *typecheck.Signature:
		return s.PtrSize
	}
	a := s.SizeofTC(T)
	if a < 1 {
		return 1
	}
	if a > s.MaxAlign {
		return s.MaxAlign
	}
	return a
}

func (s *stdSizes) OffsetsofTC(fields []*typecheck.Var) []int64 {
	offsets := make([]int64, len(fields))
	var o int64
	for i, f := range fields {
		a := s.AlignofTC(f.Type())
		o = align(o, a)
		offsets[i] = o
		o += s.SizeofTC(f.Type())
	}
	return offsets
}

func (s *stdSizes) SizeofTC(T typecheck.Type) int64 {
	switch t := T.Underlying().(type) {
	case *typecheck.Basic:
		k := t.Kind()
		if int(k) < len(tcBasicSizes) {
			if sz := tcBasicSizes[k]; sz > 0 {
				return int64(sz)
			}
		}
		if t.Info()&typecheck.IsString != 0 {
			return s.PtrSize * 3
		}
		if k == typecheck.UnsafePointer {
			return s.PtrSize
		}
		if k == typecheck.Invalid {
			return 0
		}
		panic("tccompat: unknown basic type: " + t.String())
	case *typecheck.Array:
		n := t.Len()
		if n <= 0 {
			return 0
		}
		a := s.AlignofTC(t.Elem())
		z := s.SizeofTC(t.Elem())
		return align(z, a)*(n-1) + z
	case *typecheck.Slice:
		return s.PtrSize * 3
	case *typecheck.Struct:
		n := t.NumFields()
		if n == 0 {
			return 0
		}
		fields := make([]*typecheck.Var, n)
		maxAlign := int64(1)
		for i := range fields {
			field := t.Field(i)
			fields[i] = field
			al := s.AlignofTC(field.Type())
			if al > maxAlign {
				maxAlign = al
			}
		}
		offsets := s.OffsetsofTC(fields)
		return align(offsets[n-1]+s.SizeofTC(fields[n-1].Type()), maxAlign)
	case *typecheck.Interface:
		return s.PtrSize * 2
	case *typecheck.Pointer, *typecheck.Chan, *typecheck.Map:
		return s.PtrSize
	case *typecheck.Signature:
		return s.PtrSize * 2
	default:
		panic("tccompat: unknown type for sizeof: " + T.String())
	}
}

// validateBridgeTypes cross-checks getLLVMType vs getLLVMTypeTC for all
// types in the current package. Only runs when MOXIE_VALIDATE_BRIDGE=1.
func (c *compilerContext) validateBridgeTypes() {
	if os.Getenv("MOXIE_VALIDATE_BRIDGE") != "1" {
		return
	}
	if c.mxTranslator == nil || c.pkg == nil {
		return
	}
	imp := c.mxTranslator.Importer()
	scope := c.pkg.Scope()
	var mismatches int
	for _, name := range scope.Names() {
		obj := scope.Lookup(name)
		goType := obj.Type()
		if goType == nil {
			continue
		}
		tcType := tcbridge.TranslateType(imp, goType)
		if tcType == nil {
			continue
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					fmt.Fprintf(os.Stderr, "bridge-validate: panic on %s: %v\n", name, r)
					mismatches++
				}
			}()
			old := c.getLLVMType(goType)
			new := c.getLLVMTypeTC(tcType)
			if old.String() != new.String() {
				fmt.Fprintf(os.Stderr, "bridge-validate: type mismatch for %s\n  go/types  → %s\n  typecheck → %s\n",
					name, old.String(), new.String())
				mismatches++
			}
		}()
	}
	if mismatches > 0 {
		fmt.Fprintf(os.Stderr, "bridge-validate: %d mismatches in %s\n", mismatches, c.pkg.Path())
	}
}

// getValueTC returns the LLVM value for a native SSA value.
// Parallel to getValue but operates on mxssa.Value.
func (b *builder) getValueTC(expr mxssa.Value, pos token.Pos) llvm.Value {
	switch expr := expr.(type) {
	case *mxssa.Const:
		if pos == token.NoPos && b.fn != nil {
			file := b.program.Fset.File(b.fn.Pos())
			if file != nil {
				pos = file.Pos(0)
			}
		}
		return b.createConstTC(expr, pos)
	case *mxssa.Global:
		// Look up via the old path: find the corresponding ssa.Global and delegate.
		if b.fn != nil {
			for _, m := range b.fn.Package().Members {
				if g, ok := m.(*ssa.Global); ok && g.Name() == expr.Name() {
					value := b.getGlobal(g)
					if !value.IsNil() {
						return value
					}
				}
			}
		}
		b.addError(expr.Pos(), "global not found: "+expr.Name())
		return llvm.Undef(b.getLLVMTypeTC(expr.Type()))
	default:
		if value, ok := b.mxLocals[expr]; ok {
			return value
		}
		panic("tccompat: SSA value not found in function: " + expr.String())
	}
}

// createConstTC creates an LLVM constant from a native SSA const.
func (c *compilerContext) createConstTC(expr *mxssa.Const, pos token.Pos) llvm.Value {
	switch typ := expr.Type().Underlying().(type) {
	case *typecheck.Basic:
		llvmType := c.getLLVMTypeTC(typ)
		if typ.Info()&typecheck.IsBoolean != 0 {
			n := uint64(0)
			if constant.BoolVal(expr.Value()) {
				n = 1
			}
			return llvm.ConstInt(llvmType, n, false)
		} else if typ.Info()&typecheck.IsString != 0 {
			str := constant.StringVal(expr.Value())
			strLen := llvm.ConstInt(c.uintptrType, uint64(len(str)), false)
			var strPtr llvm.Value
			if str != "" {
				objname := c.pkg.Path() + "$string"
				globalType := llvm.ArrayType(c.ctx.Int8Type(), len(str))
				global := llvm.AddGlobal(c.mod, globalType, objname)
				global.SetInitializer(c.ctx.ConstString(str, false))
				global.SetLinkage(llvm.InternalLinkage)
				global.SetGlobalConstant(true)
				global.SetUnnamedAddr(true)
				global.SetAlignment(1)
				zero := llvm.ConstInt(c.ctx.Int32Type(), 0, false)
				strPtr = llvm.ConstInBoundsGEP(globalType, global, []llvm.Value{zero, zero})
			} else {
				strPtr = llvm.ConstNull(c.dataPtrType)
			}
			return llvm.ConstNamedStruct(c.getLLVMRuntimeType("_string"), []llvm.Value{strPtr, strLen, strLen})
		} else if typ.Kind() == typecheck.UnsafePointer {
			if !expr.IsNil() {
				value, _ := constant.Uint64Val(constant.ToInt(expr.Value()))
				return llvm.ConstIntToPtr(llvm.ConstInt(c.uintptrType, value, false), c.dataPtrType)
			}
			return llvm.ConstNull(c.dataPtrType)
		} else if typ.Info()&typecheck.IsUnsigned != 0 {
			n, _ := constant.Uint64Val(constant.ToInt(expr.Value()))
			return llvm.ConstInt(llvmType, n, false)
		} else if typ.Info()&typecheck.IsInteger != 0 {
			n, _ := constant.Int64Val(constant.ToInt(expr.Value()))
			return llvm.ConstInt(llvmType, uint64(n), true)
		} else if typ.Info()&typecheck.IsFloat != 0 {
			n, _ := constant.Float64Val(expr.Value())
			return llvm.ConstFloat(llvmType, n)
		} else {
			panic("tccompat: unknown constant of basic type: " + expr.String())
		}
	case *typecheck.Chan, *typecheck.Signature, *typecheck.Interface,
		*typecheck.Pointer, *typecheck.Array, *typecheck.Slice,
		*typecheck.Struct, *typecheck.Map:
		if !expr.IsNil() {
			panic("tccompat: expected nil constant for " + typ.String())
		}
		if _, ok := typ.(*typecheck.Interface); ok {
			fields := []llvm.Value{
				llvm.ConstInt(c.uintptrType, 0, false),
				llvm.ConstPointerNull(c.dataPtrType),
			}
			return llvm.ConstNamedStruct(c.getLLVMRuntimeType("_interface"), fields)
		}
		return llvm.ConstNull(c.getLLVMTypeTC(expr.Type()))
	default:
		panic("tccompat: unhandled constant type: " + expr.Type().String())
	}
}

// getPosTC extracts token.Pos from an mxssa value.
func getPosTC(v mxssa.Value) token.Pos {
	if v == nil {
		return token.NoPos
	}
	return v.Pos()
}

// createNilCheckTC is the TC-parallel of createNilCheck.
// Skips the check for values known to be non-nil.
func (b *builder) createNilCheckTC(inst mxssa.Value, ptr llvm.Value, blockPrefix string) {
	if !ptr.IsAGlobalValue().IsNil() {
		return
	}
	switch inst := inst.(type) {
	case *mxssa.Alloc:
		return
	case *mxssa.FreeVar:
		return
	case *mxssa.IndexAddr:
		return
	case *mxssa.Convert:
		if basic, ok := inst.X.Type().Underlying().(*typecheck.Basic); ok && basic.Kind() == typecheck.UnsafePointer {
			return
		}
	}
	nilptr := llvm.ConstPointerNull(ptr.Type())
	isnil := b.CreateICmp(llvm.IntEQ, ptr, nilptr, "")
	b.createRuntimeAssert(isnil, blockPrefix, "nilPanic")
}

