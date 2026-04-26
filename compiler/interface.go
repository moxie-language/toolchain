package compiler

// This file transforms interface-related instructions (*ssa.MakeInterface,
// *ssa.TypeAssert, calls on interface types) to an intermediate IR form, to be
// lowered to the final form by the interface lowering pass. See
// interface-lowering.go for more details.

import (
	"encoding/binary"
	"fmt"
	"go/token"
	"go/types"
	"strconv"
	"strings"

	"moxie/mxssa"
	"moxie/typecheck"

	"golang.org/x/tools/go/ssa"
	"tinygo.org/x/go-llvm"
)

// Type kinds for basic types.
// They must match the constants for the Kind type in src/reflect/type.go.
var basicTypes = [...]uint8{
	types.Bool:          1,
	types.Int:           2,
	types.Int8:          3,
	types.Int16:         4,
	types.Int32:         5,
	types.Int64:         6,
	types.Uint:          7,
	types.Uint8:         8,
	types.Uint16:        9,
	types.Uint32:        10,
	types.Uint64:        11,
	types.Uintptr:       12,
	types.Float32:       13,
	types.Float64:       14,
	types.Complex64:     15,
	types.Complex128:    16,
	types.String:        17, // Moxie: kind 17 is Bytes (string=[]byte unified)
	types.UnsafePointer: 18,
}

// These must also match the constants for the Kind type in src/reflect/type.go.
const (
	typeKindChan      = 19
	typeKindInterface = 20
	typeKindPointer   = 21
	typeKindSlice     = 22
	typeKindArray     = 23
	typeKindSignature = 24
	typeKindMap       = 25
	typeKindStruct    = 26
)

// Flags stored in the first byte of the struct field byte array. Must be kept
// up to date with src/reflect/type.go.
const (
	structFieldFlagAnonymous = 1 << iota
	structFieldFlagHasTag
	structFieldFlagIsExported
	structFieldFlagIsEmbedded
)

type reflectChanDir int

const (
	refRecvDir reflectChanDir            = 1 << iota // <-chan
	refSendDir                                       // chan<-
	refBothDir = refRecvDir | refSendDir             // chan
)

// createMakeInterface emits the LLVM IR for the *ssa.MakeInterface instruction.
// It tries to put the type in the interface value, but if that's not possible,
// it will do an allocation of the right size and put that in the interface
// value field.
//
// An interface value is a {typecode, value} tuple named runtime._interface.

func (b *builder) createMakeInterfaceTC(val llvm.Value, typ typecheck.Type, pos token.Pos) llvm.Value {
	goType := b.mxTranslator.ReverseType(typ)
	if goType == nil {
		panic("createMakeInterfaceTC: no reverse type for " + typ.String())
	}
	itfValue := b.emitPointerPack([]llvm.Value{val})
	itfType := b.getTypeCode(goType)
	itf := llvm.Undef(b.getLLVMRuntimeType("_interface"))
	itf = b.CreateInsertValue(itf, itfType, 0, "")
	itf = b.CreateInsertValue(itf, itfValue, 1, "")
	return itf
}

// extractValueFromInterface extract the value from an interface value
// (runtime._interface) under the assumption that it is of the type given in
// llvmType. The behavior is undefined if the interface is nil or llvmType
// doesn't match the underlying type of the interface.
func (b *builder) extractValueFromInterface(itf llvm.Value, llvmType llvm.Type) llvm.Value {
	valuePtr := b.CreateExtractValue(itf, 1, "typeassert.value.ptr")
	return b.emitPointerUnpack(valuePtr, []llvm.Type{llvmType})[0]
}

func (c *compilerContext) pkgPathPtr(pkgpath string) llvm.Value {
	pkgpathName := "reflect/types.type.pkgpath.empty"
	if pkgpath != "" {
		pkgpathName = "reflect/types.type.pkgpath:" + pkgpath
	}

	pkgpathGlobal := c.mod.NamedGlobal(pkgpathName)
	if pkgpathGlobal.IsNil() {
		pkgpathInitializer := c.ctx.ConstString(pkgpath+"\x00", false)
		pkgpathGlobal = llvm.AddGlobal(c.mod, pkgpathInitializer.Type(), pkgpathName)
		pkgpathGlobal.SetInitializer(pkgpathInitializer)
		pkgpathGlobal.SetAlignment(1)
		pkgpathGlobal.SetUnnamedAddr(true)
		pkgpathGlobal.SetLinkage(llvm.LinkOnceODRLinkage)
		pkgpathGlobal.SetGlobalConstant(true)
	}
	pkgPathPtr := llvm.ConstGEP(pkgpathGlobal.GlobalValueType(), pkgpathGlobal, []llvm.Value{
		llvm.ConstInt(c.ctx.Int32Type(), 0, false),
		llvm.ConstInt(c.ctx.Int32Type(), 0, false),
	})

	return pkgPathPtr
}

// getTypeCode returns a reference to a type code.
// A type code is a pointer to a constant global that describes the type.
// This function returns a pointer to the 'kind' field (which might not be the
// first field in the struct).
func (c *compilerContext) getTypeCode(typ types.Type) llvm.Value {
	// Resolve alias types: alias types are resolved at compile time.
	typ = types.Unalias(typ)

	ms := c.program.MethodSets.MethodSet(typ)
	hasMethodSet := ms.Len() != 0
	_, isInterface := typ.Underlying().(*types.Interface)
	if isInterface {
		hasMethodSet = false
	}

	// As defined in https://pkg.go.dev/reflect#Type:
	// NumMethod returns the number of methods accessible using Method.
	// For a non-interface type, it returns the number of exported methods.
	// For an interface type, it returns the number of exported and unexported methods.
	var numMethods int
	for i := 0; i < ms.Len(); i++ {
		if isInterface || ms.At(i).Obj().Exported() {
			numMethods++
		}
	}

	// Short-circuit all the global pointer logic here for pointers to pointers.
	if typ, ok := typ.(*types.Pointer); ok {
		if _, ok := typ.Elem().(*types.Pointer); ok {
			// For a pointer to a pointer, we just increase the pointer by 1
			ptr := c.getTypeCode(typ.Elem())
			// if the type is already *****T or higher, we can't make it.
			if typstr := typ.String(); strings.HasPrefix(typstr, "*****") {
				c.addError(token.NoPos, fmt.Sprintf("too many levels of pointers for typecode: %s", typstr))
			}
			return llvm.ConstGEP(c.ctx.Int8Type(), ptr, []llvm.Value{
				llvm.ConstInt(c.ctx.Int32Type(), 1, false),
			})
		}
	}

	typeCodeName, isLocal := getTypeCodeName(typ)
	globalName := "reflect/types.type:" + typeCodeName
	var global llvm.Value
	if isLocal {
		// This type is a named type inside a function, like this:
		//
		//     func foo() any {
		//         type named int
		//         return named(0)
		//     }
		if obj := c.interfaceTypes.At(typ); obj != nil {
			global = obj.(llvm.Value)
		}
	} else {
		// Regular type (named or otherwise).
		global = c.mod.NamedGlobal(globalName)
	}
	if global.IsNil() {
		var typeFields []llvm.Value
		// Define the type fields. These must match the structs in
		// src/reflect/type.go (ptrType, arrayType, etc). See the comment at the
		// top of src/reflect/type.go for more information on the layout of these structs.
		typeFieldTypes := []*types.Var{
			types.NewVar(token.NoPos, nil, "kind", types.Typ[types.Int8]),
		}
		switch typ := typ.(type) {
		case *types.Basic:
			typeFieldTypes = append(typeFieldTypes,
				types.NewVar(token.NoPos, nil, "ptrTo", types.Typ[types.UnsafePointer]),
			)
		case *types.Named:
			name := typ.Obj().Name()
			var pkgname string
			if pkg := typ.Obj().Pkg(); pkg != nil {
				pkgname = pkg.Name()
			}
			typeFieldTypes = append(typeFieldTypes,
				types.NewVar(token.NoPos, nil, "numMethods", types.Typ[types.Uint16]),
				types.NewVar(token.NoPos, nil, "ptrTo", types.Typ[types.UnsafePointer]),
				types.NewVar(token.NoPos, nil, "underlying", types.Typ[types.UnsafePointer]),
				types.NewVar(token.NoPos, nil, "pkgpath", types.Typ[types.UnsafePointer]),
				types.NewVar(token.NoPos, nil, "name", types.NewArray(types.Typ[types.Int8], int64(len(pkgname)+1+len(name)+1))),
			)
		case *types.Chan:
			typeFieldTypes = append(typeFieldTypes,
				types.NewVar(token.NoPos, nil, "numMethods", types.Typ[types.Uint16]), // reuse for select chan direction
				types.NewVar(token.NoPos, nil, "ptrTo", types.Typ[types.UnsafePointer]),
				types.NewVar(token.NoPos, nil, "elementType", types.Typ[types.UnsafePointer]),
			)
		case *types.Slice:
			typeFieldTypes = append(typeFieldTypes,
				types.NewVar(token.NoPos, nil, "numMethods", types.Typ[types.Uint16]),
				types.NewVar(token.NoPos, nil, "ptrTo", types.Typ[types.UnsafePointer]),
				types.NewVar(token.NoPos, nil, "elementType", types.Typ[types.UnsafePointer]),
			)
		case *types.Pointer:
			typeFieldTypes = append(typeFieldTypes,
				types.NewVar(token.NoPos, nil, "numMethods", types.Typ[types.Uint16]),
				types.NewVar(token.NoPos, nil, "elementType", types.Typ[types.UnsafePointer]),
			)
		case *types.Array:
			typeFieldTypes = append(typeFieldTypes,
				types.NewVar(token.NoPos, nil, "numMethods", types.Typ[types.Uint16]),
				types.NewVar(token.NoPos, nil, "ptrTo", types.Typ[types.UnsafePointer]),
				types.NewVar(token.NoPos, nil, "elementType", types.Typ[types.UnsafePointer]),
				types.NewVar(token.NoPos, nil, "length", types.Typ[types.Uintptr]),
				types.NewVar(token.NoPos, nil, "sliceOf", types.Typ[types.UnsafePointer]),
			)
		case *types.Map:
			typeFieldTypes = append(typeFieldTypes,
				types.NewVar(token.NoPos, nil, "numMethods", types.Typ[types.Uint16]),
				types.NewVar(token.NoPos, nil, "ptrTo", types.Typ[types.UnsafePointer]),
				types.NewVar(token.NoPos, nil, "elementType", types.Typ[types.UnsafePointer]),
				types.NewVar(token.NoPos, nil, "keyType", types.Typ[types.UnsafePointer]),
			)
		case *types.Struct:
			typeFieldTypes = append(typeFieldTypes,
				types.NewVar(token.NoPos, nil, "numMethods", types.Typ[types.Uint16]),
				types.NewVar(token.NoPos, nil, "ptrTo", types.Typ[types.UnsafePointer]),
				types.NewVar(token.NoPos, nil, "pkgpath", types.Typ[types.UnsafePointer]),
				types.NewVar(token.NoPos, nil, "size", types.Typ[types.Uint32]),
				types.NewVar(token.NoPos, nil, "numFields", types.Typ[types.Uint16]),
				types.NewVar(token.NoPos, nil, "fields", types.NewArray(c.getRuntimeType("structField"), int64(typ.NumFields()))),
			)
		case *types.Interface:
			typeFieldTypes = append(typeFieldTypes,
				types.NewVar(token.NoPos, nil, "ptrTo", types.Typ[types.UnsafePointer]),
			)
			// TODO: methods
		case *types.Signature:
			typeFieldTypes = append(typeFieldTypes,
				types.NewVar(token.NoPos, nil, "ptrTo", types.Typ[types.UnsafePointer]),
			)
			// TODO: signature params and return values
		}
		if hasMethodSet {
			// This method set is appended at the start of the struct. It is
			// removed in the interface lowering pass.
			// TODO: don't remove these and instead do what upstream Go is doing
			// instead. See: https://research.swtch.com/interfaces. This can
			// likely be optimized in LLVM using
			// https://llvm.org/docs/TypeMetadata.html.
			typeFieldTypes = append([]*types.Var{
				types.NewVar(token.NoPos, nil, "methodSet", types.Typ[types.UnsafePointer]),
			}, typeFieldTypes...)
		}
		globalType := types.NewStruct(typeFieldTypes, nil)
		global = llvm.AddGlobal(c.mod, c.getLLVMType(globalType), globalName)
		if isLocal {
			c.interfaceTypes.Set(typ, global)
		}
		metabyte := getTypeKind(typ)

		// Precompute these so we don't have to calculate them at runtime.
		if types.Comparable(typ) {
			metabyte |= 1 << 6
		}

		if hashmapIsBinaryKey(typ) {
			metabyte |= 1 << 7
		}

		switch typ := typ.(type) {
		case *types.Basic:
			typeFields = []llvm.Value{c.getTypeCode(types.NewPointer(typ))}
		case *types.Named:
			name := typ.Obj().Name()
			var pkgpath string
			var pkgname string
			if pkg := typ.Obj().Pkg(); pkg != nil {
				pkgpath = pkg.Path()
				pkgname = pkg.Name()
			}
			pkgPathPtr := c.pkgPathPtr(pkgpath)
			typeFields = []llvm.Value{
				llvm.ConstInt(c.ctx.Int16Type(), uint64(numMethods), false), // numMethods
				c.getTypeCode(types.NewPointer(typ)),                        // ptrTo
				c.getTypeCode(typ.Underlying()),                             // underlying
				pkgPathPtr,                                                  // pkgpath pointer
				c.ctx.ConstString(pkgname+"."+name+"\x00", false),           // name
			}
			metabyte |= 1 << 5 // "named" flag
		case *types.Chan:
			var dir reflectChanDir
			switch typ.Dir() {
			case types.SendRecv:
				dir = refBothDir
			case types.RecvOnly:
				dir = refRecvDir
			case types.SendOnly:
				dir = refSendDir
			}

			typeFields = []llvm.Value{
				llvm.ConstInt(c.ctx.Int16Type(), uint64(dir), false), // actually channel direction
				c.getTypeCode(types.NewPointer(typ)),                 // ptrTo
				c.getTypeCode(typ.Elem()),                            // elementType
			}
		case *types.Slice:
			typeFields = []llvm.Value{
				llvm.ConstInt(c.ctx.Int16Type(), 0, false), // numMethods
				c.getTypeCode(types.NewPointer(typ)),       // ptrTo
				c.getTypeCode(typ.Elem()),                  // elementType
			}
		case *types.Pointer:
			typeFields = []llvm.Value{
				llvm.ConstInt(c.ctx.Int16Type(), uint64(numMethods), false), // numMethods
				c.getTypeCode(typ.Elem()),
			}
		case *types.Array:
			typeFields = []llvm.Value{
				llvm.ConstInt(c.ctx.Int16Type(), 0, false),             // numMethods
				c.getTypeCode(types.NewPointer(typ)),                   // ptrTo
				c.getTypeCode(typ.Elem()),                              // elementType
				llvm.ConstInt(c.uintptrType, uint64(typ.Len()), false), // length
				c.getTypeCode(types.NewSlice(typ.Elem())),              // slicePtr
			}
		case *types.Map:
			typeFields = []llvm.Value{
				llvm.ConstInt(c.ctx.Int16Type(), 0, false), // numMethods
				c.getTypeCode(types.NewPointer(typ)),       // ptrTo
				c.getTypeCode(typ.Elem()),                  // elem
				c.getTypeCode(typ.Key()),                   // key
			}
		case *types.Struct:
			var pkgpath string
			if typ.NumFields() > 0 {
				if pkg := typ.Field(0).Pkg(); pkg != nil {
					pkgpath = pkg.Path()
				}
			}
			pkgPathPtr := c.pkgPathPtr(pkgpath)

			llvmStructType := c.getLLVMType(typ)
			size := c.targetData.TypeStoreSize(llvmStructType)
			typeFields = []llvm.Value{
				llvm.ConstInt(c.ctx.Int16Type(), uint64(numMethods), false), // numMethods
				c.getTypeCode(types.NewPointer(typ)),                        // ptrTo
				pkgPathPtr,
				llvm.ConstInt(c.ctx.Int32Type(), uint64(size), false),            // size
				llvm.ConstInt(c.ctx.Int16Type(), uint64(typ.NumFields()), false), // numFields
			}
			structFieldType := c.getLLVMRuntimeType("structField")

			var fields []llvm.Value
			for i := 0; i < typ.NumFields(); i++ {
				field := typ.Field(i)
				offset := c.targetData.ElementOffset(llvmStructType, i)
				var flags uint8
				if field.Anonymous() {
					flags |= structFieldFlagAnonymous
				}
				if typ.Tag(i) != "" {
					flags |= structFieldFlagHasTag
				}
				if token.IsExported(field.Name()) {
					flags |= structFieldFlagIsExported
				}
				if field.Embedded() {
					flags |= structFieldFlagIsEmbedded
				}

				var offsBytes [binary.MaxVarintLen32]byte
				offLen := binary.PutUvarint(offsBytes[:], offset)

				data := string(flags) + string(offsBytes[:offLen]) + field.Name() + "\x00"
				if typ.Tag(i) != "" {
					if len(typ.Tag(i)) > 0xff {
						c.addError(field.Pos(), fmt.Sprintf("struct tag is %d bytes which is too long, max is 255", len(typ.Tag(i))))
					}
					data += string([]byte{byte(len(typ.Tag(i)))}) + typ.Tag(i)
				}
				dataInitializer := c.ctx.ConstString(data, false)
				dataGlobal := llvm.AddGlobal(c.mod, dataInitializer.Type(), globalName+"."+field.Name())
				dataGlobal.SetInitializer(dataInitializer)
				dataGlobal.SetAlignment(1)
				dataGlobal.SetUnnamedAddr(true)
				dataGlobal.SetLinkage(llvm.InternalLinkage)
				dataGlobal.SetGlobalConstant(true)
				fieldType := c.getTypeCode(field.Type())
				fields = append(fields, llvm.ConstNamedStruct(structFieldType, []llvm.Value{
					fieldType,
					llvm.ConstGEP(dataGlobal.GlobalValueType(), dataGlobal, []llvm.Value{
						llvm.ConstInt(c.ctx.Int32Type(), 0, false),
						llvm.ConstInt(c.ctx.Int32Type(), 0, false),
					}),
				}))
			}
			typeFields = append(typeFields, llvm.ConstArray(structFieldType, fields))
		case *types.Interface:
			typeFields = []llvm.Value{c.getTypeCode(types.NewPointer(typ))}
			// TODO: methods
		case *types.Signature:
			typeFields = []llvm.Value{c.getTypeCode(types.NewPointer(typ))}
			// TODO: params, return values, etc
		}
		// Prepend metadata byte.
		typeFields = append([]llvm.Value{
			llvm.ConstInt(c.ctx.Int8Type(), uint64(metabyte), false),
		}, typeFields...)
		if hasMethodSet {
			typeFields = append([]llvm.Value{
				c.getTypeMethodSet(typ),
			}, typeFields...)
		}
		alignment := c.targetData.TypeAllocSize(c.dataPtrType)
		if alignment < 4 {
			alignment = 4
		}
		globalValue := c.ctx.ConstStruct(typeFields, false)
		global.SetInitializer(globalValue)
		if isLocal {
			global.SetLinkage(llvm.InternalLinkage)
		} else {
			global.SetLinkage(llvm.LinkOnceODRLinkage)
		}
		global.SetGlobalConstant(true)
		global.SetAlignment(int(alignment))
		if c.Debug {
			file := c.getDIFile("<Go type>")
			diglobal := c.dibuilder.CreateGlobalVariableExpression(file, llvm.DIGlobalVariableExpression{
				Name:        "type " + typ.String(),
				File:        file,
				Line:        1,
				Type:        c.getDIType(globalType),
				LocalToUnit: false,
				Expr:        c.dibuilder.CreateExpression(nil),
				AlignInBits: uint32(alignment * 8),
			})
			global.AddMetadata(0, diglobal)
		}
	}
	offset := uint64(0)
	if hasMethodSet {
		// The pointer to the method set is always the first element of the
		// global (if there is a method set). However, the pointer we return
		// should point to the 'kind' field not the method set.
		offset = 1
	}
	return llvm.ConstGEP(global.GlobalValueType(), global, []llvm.Value{
		llvm.ConstInt(c.ctx.Int32Type(), 0, false),
		llvm.ConstInt(c.ctx.Int32Type(), offset, false),
	})
}

// getTypeKind returns the type kind for the given type, as defined by
// reflect.Kind.
func getTypeKind(t types.Type) uint8 {
	switch t := t.Underlying().(type) {
	case *types.Basic:
		return basicTypes[t.Kind()]
	case *types.Chan:
		return typeKindChan
	case *types.Interface:
		return typeKindInterface
	case *types.Pointer:
		return typeKindPointer
	case *types.Slice:
		// Moxie: []byte reports as String kind (string=[]byte unification).
		if b, ok := t.Elem().(*types.Basic); ok && b.Kind() == types.Byte {
			return basicTypes[types.String]
		}
		return typeKindSlice
	case *types.Array:
		return typeKindArray
	case *types.Signature:
		return typeKindSignature
	case *types.Map:
		return typeKindMap
	case *types.Struct:
		return typeKindStruct
	default:
		panic("unknown type")
	}
}

var basicTypeNames = [...]string{
	types.Bool:          "bool",
	types.Int:           "int",
	types.Int8:          "int8",
	types.Int16:         "int16",
	types.Int32:         "int32",
	types.Int64:         "int64",
	types.Uint:          "uint",
	types.Uint8:         "uint8",
	types.Uint16:        "uint16",
	types.Uint32:        "uint32",
	types.Uint64:        "uint64",
	types.Uintptr:       "uintptr",
	types.Float32:       "float32",
	types.Float64:       "float64",
	types.Complex64:     "complex64",
	types.Complex128:    "complex128",
	types.String:        "bytes", // Moxie: unified string=[]byte type
	types.UnsafePointer: "unsafe.Pointer",
}

// getTypeCodeName returns a name for this type that can be used in the
// interface lowering pass to assign type codes as expected by the reflect
// package. See getTypeCodeNum.
func getTypeCodeName(t types.Type) (string, bool) {
	switch t := types.Unalias(t).(type) {
	case *types.Named:
		if t.Obj().Parent() != t.Obj().Pkg().Scope() {
			return "named:" + t.String() + "$local", true
		}
		return "named:" + t.String(), false
	case *types.Array:
		s, isLocal := getTypeCodeName(t.Elem())
		return "array:" + strconv.FormatInt(t.Len(), 10) + ":" + s, isLocal
	case *types.Basic:
		return "basic:" + basicTypeNames[t.Kind()], false
	case *types.Chan:
		s, isLocal := getTypeCodeName(t.Elem())
		var dir string
		switch t.Dir() {
		case types.SendOnly:
			dir = "s:"
		case types.RecvOnly:
			dir = "r:"
		case types.SendRecv:
			dir = "sr:"
		}

		return "chan:" + dir + s, isLocal
	case *types.Interface:
		isLocal := false
		methods := make([]string, t.NumMethods())
		for i := 0; i < t.NumMethods(); i++ {
			name := t.Method(i).Name()
			if !token.IsExported(name) {
				name = t.Method(i).Pkg().Path() + "." + name
			}
			s, local := getTypeCodeName(t.Method(i).Type())
			if local {
				isLocal = true
			}
			methods[i] = name + ":" + s
		}
		return "interface:" + "{" + strings.Join(methods, ",") + "}", isLocal
	case *types.Map:
		keyType, keyLocal := getTypeCodeName(t.Key())
		elemType, elemLocal := getTypeCodeName(t.Elem())
		return "map:" + "{" + keyType + "," + elemType + "}", keyLocal || elemLocal
	case *types.Pointer:
		s, isLocal := getTypeCodeName(t.Elem())
		return "pointer:" + s, isLocal
	case *types.Signature:
		isLocal := false
		params := make([]string, t.Params().Len())
		for i := 0; i < t.Params().Len(); i++ {
			s, local := getTypeCodeName(t.Params().At(i).Type())
			if local {
				isLocal = true
			}
			params[i] = s
		}
		results := make([]string, t.Results().Len())
		for i := 0; i < t.Results().Len(); i++ {
			s, local := getTypeCodeName(t.Results().At(i).Type())
			if local {
				isLocal = true
			}
			results[i] = s
		}
		return "func:" + "{" + strings.Join(params, ",") + "}{" + strings.Join(results, ",") + "}", isLocal
	case *types.Slice:
		// Moxie: []byte uses the unified bytes type code name.
		if b, ok := t.Elem().(*types.Basic); ok && b.Kind() == types.Byte {
			return "basic:bytes", false
		}
		s, isLocal := getTypeCodeName(t.Elem())
		return "slice:" + s, isLocal
	case *types.Struct:
		elems := make([]string, t.NumFields())
		isLocal := false
		for i := 0; i < t.NumFields(); i++ {
			embedded := ""
			if t.Field(i).Embedded() {
				embedded = "#"
			}
			s, local := getTypeCodeName(t.Field(i).Type())
			if local {
				isLocal = true
			}
			elems[i] = embedded + t.Field(i).Name() + ":" + s
			if t.Tag(i) != "" {
				elems[i] += "`" + t.Tag(i) + "`"
			}
		}
		return "struct:" + "{" + strings.Join(elems, ",") + "}", isLocal
	default:
		panic("unknown type: " + t.String())
	}
}

// getTypeMethodSet returns a reference (GEP) to a global method set. This
// method set should be unreferenced after the interface lowering pass.
func (c *compilerContext) getTypeMethodSet(typ types.Type) llvm.Value {
	globalName := typ.String() + "$methodset"
	global := c.mod.NamedGlobal(globalName)
	if global.IsNil() {
		ms := c.program.MethodSets.MethodSet(typ)

		// Create method set.
		var signatures, wrappers []llvm.Value
		for i := 0; i < ms.Len(); i++ {
			method := ms.At(i)
			signatureGlobal := c.getMethodSignature(method.Obj().(*types.Func))
			signatures = append(signatures, signatureGlobal)
			fn := c.program.MethodValue(method)
			mxFn := c.mxTranslator.EnsureFunction(fn)
			llvmFnType, llvmFn := c.getFunction(mxFn)
			if llvmFn.IsNil() {
				// compiler error, so panic
				panic("cannot find function: " + c.getFunctionInfo(mxFn).linkName)
			}
			wrapper := c.getInterfaceInvokeWrapper(fn, llvmFnType, llvmFn)
			wrappers = append(wrappers, wrapper)
		}

		// Construct global value.
		globalValue := c.ctx.ConstStruct([]llvm.Value{
			llvm.ConstInt(c.uintptrType, uint64(ms.Len()), false),
			llvm.ConstArray(c.dataPtrType, signatures),
			c.ctx.ConstStruct(wrappers, false),
		}, false)
		global = llvm.AddGlobal(c.mod, globalValue.Type(), globalName)
		global.SetInitializer(globalValue)
		global.SetGlobalConstant(true)
		global.SetUnnamedAddr(true)
		global.SetLinkage(llvm.LinkOnceODRLinkage)
	}
	return global
}

// getMethodSignatureName returns a unique name (that can be used as the name of
// a global) for the given method.
func (c *compilerContext) getMethodSignatureName(method *types.Func) string {
	signature := methodSignature(method)
	var globalName string
	if token.IsExported(method.Name()) {
		globalName = "reflect/methods." + signature
	} else {
		globalName = method.Type().(*types.Signature).Recv().Pkg().Path() + ".$methods." + signature
	}
	return globalName
}

// getMethodSignature returns a global variable which is a reference to an
// external *i8 indicating the indicating the signature of this method. It is
// used during the interface lowering pass.
func (c *compilerContext) getMethodSignature(method *types.Func) llvm.Value {
	globalName := c.getMethodSignatureName(method)
	signatureGlobal := c.mod.NamedGlobal(globalName)
	if signatureGlobal.IsNil() {
		// TODO: put something useful in these globals, such as the method
		// signature. Useful to one day implement reflect.Value.Method(n).
		signatureGlobal = llvm.AddGlobal(c.mod, c.ctx.Int8Type(), globalName)
		signatureGlobal.SetInitializer(llvm.ConstInt(c.ctx.Int8Type(), 0, false))
		signatureGlobal.SetLinkage(llvm.LinkOnceODRLinkage)
		signatureGlobal.SetGlobalConstant(true)
		signatureGlobal.SetAlignment(1)
	}
	return signatureGlobal
}

// createTypeAssert will emit the code for a typeassert, used in if statements
// and in type switches (Go SSA does not have type switches, only if/else
// chains). Note that even though the Go SSA does not contain type switches,
// LLVM will recognize the pattern and make it a real switch in many cases.
//
// Type asserts on concrete types are trivial: just compare type numbers. Type
// asserts on interfaces are more difficult, see the comments in the function.

func (b *builder) createTypeAssertTC(expr *mxssa.TypeAssert) llvm.Value {
	itf := b.getValueTC(expr.X, expr.Pos())
	assertedType := b.getLLVMTypeTC(expr.AssertedType)

	actualTypeNum := b.CreateExtractValue(itf, 0, "interface.type")
	commaOk := llvm.Value{}

	goAssertedType := b.mxTranslator.ReverseType(expr.AssertedType)
	if goAssertedType == nil {
		panic("createTypeAssertTC: no reverse type for " + expr.AssertedType.String())
	}

	if _, ok := expr.AssertedType.Underlying().(*typecheck.Interface); ok {
		tcItf := expr.AssertedType.Underlying().(*typecheck.Interface)
		if tcItf.NumMethods() == 0 {
			commaOk = llvm.ConstInt(b.ctx.Int1Type(), 1, true)
		} else {
			fn := b.getInterfaceImplementsFunc(goAssertedType)
			commaOk = b.CreateCall(fn.GlobalValueType(), fn, []llvm.Value{actualTypeNum}, "")
		}
	} else {
		name, _ := getTypeCodeName(goAssertedType)
		globalName := "reflect/types.typeid:" + name
		assertedTypeCodeGlobal := b.mod.NamedGlobal(globalName)
		if assertedTypeCodeGlobal.IsNil() {
			assertedTypeCodeGlobal = llvm.AddGlobal(b.mod, b.ctx.Int8Type(), globalName)
			assertedTypeCodeGlobal.SetGlobalConstant(true)
		}
		commaOk = b.createRuntimeCall("typeAssert", []llvm.Value{actualTypeNum, assertedTypeCodeGlobal}, "typecode")
	}

	prevBlock := b.GetInsertBlock()
	okBlock := b.insertBasicBlock("typeassert.ok")
	nextBlock := b.insertBasicBlock("typeassert.next")
	b.currentBlockInfo.exit = nextBlock
	b.CreateCondBr(commaOk, okBlock, nextBlock)

	b.SetInsertPointAtEnd(okBlock)
	var valueOk llvm.Value
	if _, ok := expr.AssertedType.Underlying().(*typecheck.Interface); ok {
		valueOk = itf
	} else {
		valueOk = b.extractValueFromInterface(itf, assertedType)
	}
	b.CreateBr(nextBlock)

	b.SetInsertPointAtEnd(nextBlock)
	phi := b.CreatePHI(assertedType, "typeassert.value")
	phi.AddIncoming([]llvm.Value{llvm.ConstNull(assertedType), valueOk}, []llvm.BasicBlock{prevBlock, okBlock})

	if expr.CommaOk {
		tuple := b.ctx.ConstStruct([]llvm.Value{llvm.Undef(assertedType), llvm.Undef(b.ctx.Int1Type())}, false)
		tuple = b.CreateInsertValue(tuple, phi, 0, "")
		tuple = b.CreateInsertValue(tuple, commaOk, 1, "")
		return tuple
	} else {
		b.createRuntimeCall("interfaceTypeAssert", []llvm.Value{commaOk}, "")
		return phi
	}
}

// getMethodsString returns a string to be used in the "moxie-methods" string
// attribute for interface functions.
func (c *compilerContext) getMethodsString(itf *types.Interface) string {
	methods := make([]string, itf.NumMethods())
	for i := range methods {
		methods[i] = c.getMethodSignatureName(itf.Method(i))
	}
	return strings.Join(methods, "; ")
}

// getInterfaceImplementsFunc returns a declared function that works as a type
// switch. The interface lowering pass will define this function.
func (c *compilerContext) getInterfaceImplementsFunc(assertedType types.Type) llvm.Value {
	s, _ := getTypeCodeName(assertedType.Underlying())
	fnName := s + ".$typeassert"
	llvmFn := c.mod.NamedFunction(fnName)
	if llvmFn.IsNil() {
		llvmFnType := llvm.FunctionType(c.ctx.Int1Type(), []llvm.Type{c.dataPtrType}, false)
		llvmFn = llvm.AddFunction(c.mod, fnName, llvmFnType)
		c.addStandardDeclaredAttributes(llvmFn)
		methods := c.getMethodsString(assertedType.Underlying().(*types.Interface))
		llvmFn.AddFunctionAttr(c.ctx.CreateStringAttribute("moxie-methods", methods))
	}
	return llvmFn
}

// getInvokeFunction returns the thunk to call the given interface method. The
// thunk is declared, not defined: it will be defined by the interface lowering
// pass.

func (c *compilerContext) getInvokeFunctionTC(instr *mxssa.CallCommon) llvm.Value {
	itfType := instr.Value.Type().Underlying().(*typecheck.Interface)
	if itfType.NumMethods() == 0 {
		panic("getInvokeFunctionTC: invoke on empty interface for method " +
			instr.Method.Name() + " (compiler invariant broken)")
	}
	goItfType := c.mxTranslator.ReverseType(instr.Value.Type().Underlying())
	if goItfType == nil {
		panic("getInvokeFunctionTC: no reverse type for " + instr.Value.Type().String())
	}
	goItf := goItfType.(*types.Interface)
	s, _ := getTypeCodeName(goItfType)
	fnName := s + "." + instr.Method.Name() + "$invoke"
	llvmFn := c.mod.NamedFunction(fnName)
	if llvmFn.IsNil() {
		goMethodType := c.mxTranslator.ReverseType(instr.Method.Type())
		if goMethodType == nil {
			panic("getInvokeFunctionTC: no reverse type for method " + instr.Method.Name())
		}
		// Convert via tcbridge: instr.Method.Signature() can have nil
		// params (the typecheck.Func may not have full signature info),
		// so go through the go/types side which is reliable.
		sig := goMethodType.(*types.Signature)
		var paramTuple []*typecheck.Var
		for i := 0; i < sig.Params().Len(); i++ {
			tcParamType := c.mxTranslator.ForwardType(sig.Params().At(i).Type())
			paramTuple = append(paramTuple, typecheck.NewVar(nil, sig.Params().At(i).Name(), tcParamType))
		}
		paramTuple = append(paramTuple, typecheck.NewVar(nil, "$typecode", typecheck.Typ[typecheck.UnsafePointer]))
		var tcRecv *typecheck.Var
		if sig.Recv() != nil {
			tcRecvType := c.mxTranslator.ForwardType(sig.Recv().Type())
			tcRecv = typecheck.NewVar(nil, sig.Recv().Name(), tcRecvType)
		}
		var tcResults *typecheck.Tuple
		if sig.Results() != nil {
			var resultVars []*typecheck.Var
			for i := 0; i < sig.Results().Len(); i++ {
				tcResType := c.mxTranslator.ForwardType(sig.Results().At(i).Type())
				resultVars = append(resultVars, typecheck.NewVar(nil, sig.Results().At(i).Name(), tcResType))
			}
			tcResults = typecheck.NewTuple(resultVars...)
		}
		llvmFnType := c.getLLVMFunctionTypeTC(typecheck.NewSignature(tcRecv, typecheck.NewTuple(paramTuple...), tcResults, false))
		llvmFn = llvm.AddFunction(c.mod, fnName, llvmFnType)
		c.addStandardDeclaredAttributes(llvmFn)
		goMethod := findGoMethod(goItf, instr.Method.Name())
		if goMethod == nil {
			panic("getInvokeFunctionTC: cannot find go/types method " + instr.Method.Name())
		}
		llvmFn.AddFunctionAttr(c.ctx.CreateStringAttribute("moxie-invoke", c.getMethodSignatureName(goMethod)))
		methods := c.getMethodsString(goItf)
		llvmFn.AddFunctionAttr(c.ctx.CreateStringAttribute("moxie-methods", methods))
	}
	return llvmFn
}

func findGoMethod(itf *types.Interface, name string) *types.Func {
	for i := 0; i < itf.NumMethods(); i++ {
		if itf.Method(i).Name() == name {
			return itf.Method(i)
		}
	}
	return nil
}

// getInterfaceInvokeWrapper returns a wrapper for the given method so it can be
// invoked from an interface. The wrapper takes in a pointer to the underlying
// value, dereferences or unpacks it if necessary, and calls the real method.
// If the method to wrap has a pointer receiver, no wrapping is necessary and
// the function is returned directly.
func (c *compilerContext) getInterfaceInvokeWrapper(fn *ssa.Function, llvmFnType llvm.Type, llvmFn llvm.Value) llvm.Value {
	wrapperName := llvmFn.Name() + "$invoke"
	wrapper := c.mod.NamedFunction(wrapperName)
	if !wrapper.IsNil() {
		// Wrapper already created. Return it directly.
		return wrapper
	}

	// Get the expanded receiver type.
	receiverType := c.getLLVMType(fn.Signature.Recv().Type())
	var expandedReceiverType []llvm.Type
	for _, info := range c.expandFormalParamTypeTC(receiverType, "", nil) {
		expandedReceiverType = append(expandedReceiverType, info.llvmType)
	}

	// Does this method even need any wrapping?
	if len(expandedReceiverType) == 1 && receiverType.TypeKind() == llvm.PointerTypeKind {
		// Nothing to wrap.
		// Casting a function signature to a different signature and calling it
		// with a receiver pointer bitcasted to *i8 (as done in calls on an
		// interface) is hopefully a safe (defined) operation.
		return llvmFn
	}

	// create wrapper function
	paramTypes := append([]llvm.Type{c.dataPtrType}, llvmFnType.ParamTypes()[len(expandedReceiverType):]...)
	wrapFnType := llvm.FunctionType(llvmFnType.ReturnType(), paramTypes, false)
	wrapper = llvm.AddFunction(c.mod, wrapperName, wrapFnType)
	c.addStandardAttributes(wrapper)

	wrapper.SetLinkage(llvm.LinkOnceODRLinkage)
	wrapper.SetUnnamedAddr(true)

	// Create a new builder just to create this wrapper.
	b := builder{
		compilerContext: c,
		Builder:         c.ctx.NewBuilder(),
	}
	defer b.Builder.Dispose()

	// add debug info if needed
	if c.Debug {
		pos := c.mxProgram.Fset.Position(fn.Pos())
		mxFn := c.mxTranslator.EnsureFunction(fn)
		difunc := c.attachDebugInfoRaw(mxFn, wrapper, "$invoke", pos.Filename, pos.Line)
		b.SetCurrentDebugLocation(uint(pos.Line), uint(pos.Column), difunc, llvm.Metadata{})
	}

	// set up IR builder
	block := b.ctx.AddBasicBlock(wrapper, "entry")
	b.SetInsertPointAtEnd(block)

	receiverValue := b.emitPointerUnpack(wrapper.Param(0), []llvm.Type{receiverType})[0]
	params := append(b.expandFormalParam(receiverValue), wrapper.Params()[1:]...)
	if llvmFnType.ReturnType().TypeKind() == llvm.VoidTypeKind {
		b.CreateCall(llvmFnType, llvmFn, params, "")
		b.CreateRetVoid()
	} else {
		ret := b.CreateCall(llvmFnType, llvmFn, params, "ret")
		b.CreateRet(ret)
	}

	return wrapper
}

// methodSignature creates a readable version of a method signature (including
// the function name, excluding the receiver name). This string is used
// internally to match interfaces and to call the correct method on an
// interface. Examples:
//
//	String() string
//	Read([]byte) (int, error)
func methodSignature(method *types.Func) string {
	return method.Name() + signature(method.Type().(*types.Signature))
}

// Make a readable version of a function (pointer) signature.
// Examples:
//
//	() string
//	(string, int) (int, error)
func signature(sig *types.Signature) string {
	s := ""
	if sig.Params().Len() == 0 {
		s += "()"
	} else {
		s += "("
		for i := 0; i < sig.Params().Len(); i++ {
			if i > 0 {
				s += ", "
			}
			s += typestring(sig.Params().At(i).Type())
		}
		s += ")"
	}
	if sig.Results().Len() == 0 {
		// keep as-is
	} else if sig.Results().Len() == 1 {
		s += " " + typestring(sig.Results().At(0).Type())
	} else {
		s += " ("
		for i := 0; i < sig.Results().Len(); i++ {
			if i > 0 {
				s += ", "
			}
			s += typestring(sig.Results().At(i).Type())
		}
		s += ")"
	}
	return s
}

// typestring returns a stable (human-readable) type string for the given type
// that can be used for interface equality checks. It is almost (but not
// exactly) the same as calling t.String(). The main difference is some
// normalization around `byte` vs `uint8` for example.
func typestring(t types.Type) string {
	// See: https://github.com/golang/go/blob/master/src/go/types/typestring.go
	switch t := types.Unalias(t).(type) {
	case *types.Array:
		return "[" + strconv.FormatInt(t.Len(), 10) + "]" + typestring(t.Elem())
	case *types.Basic:
		return basicTypeNames[t.Kind()]
	case *types.Chan:
		switch t.Dir() {
		case types.SendRecv:
			return "chan (" + typestring(t.Elem()) + ")"
		case types.SendOnly:
			return "chan<- (" + typestring(t.Elem()) + ")"
		case types.RecvOnly:
			return "<-chan (" + typestring(t.Elem()) + ")"
		default:
			panic("unknown channel direction")
		}
	case *types.Interface:
		methods := make([]string, t.NumMethods())
		for i := range methods {
			method := t.Method(i)
			methods[i] = method.Name() + signature(method.Type().(*types.Signature))
		}
		return "interface{" + strings.Join(methods, ";") + "}"
	case *types.Map:
		return "map[" + typestring(t.Key()) + "]" + typestring(t.Elem())
	case *types.Named:
		return t.String()
	case *types.Pointer:
		return "*" + typestring(t.Elem())
	case *types.Signature:
		return "func" + signature(t)
	case *types.Slice:
		return "[]" + typestring(t.Elem())
	case *types.Struct:
		fields := make([]string, t.NumFields())
		for i := range fields {
			field := t.Field(i)
			fields[i] = field.Name() + " " + typestring(field.Type())
			if tag := t.Tag(i); tag != "" {
				fields[i] += " " + strconv.Quote(tag)
			}
		}
		return "struct{" + strings.Join(fields, ";") + "}"
	default:
		panic("unknown type: " + t.String())
	}
}
