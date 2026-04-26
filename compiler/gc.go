package compiler

// This file provides IR transformations necessary for precise and portable
// garbage collectors.

import (
	"go/token"

	"moxie/mxssa"
	"tinygo.org/x/go-llvm"
)

func (b *builder) trackExprTC(expr mxssa.Value, value llvm.Value) {
	switch expr := expr.(type) {
	case *mxssa.Alloc, *mxssa.MakeChan, *mxssa.MakeMap:
		b.trackPointer(value)
	case *mxssa.Call, *mxssa.Convert, *mxssa.MakeClosure, *mxssa.MakeInterface, *mxssa.MakeSlice, *mxssa.Next:
		if !value.IsNil() {
			b.trackValue(value)
		}
	case *mxssa.Select:
		if alloca, ok := b.selectRecvBufTC[expr]; ok {
			if alloca.IsAUndefValue().IsNil() {
				b.trackPointer(alloca)
			}
		}
	case *mxssa.UnOp:
		switch expr.Op {
		case token.MUL:
			b.trackValue(value)
		case token.ARROW:
			b.trackValue(value)
		}
	case *mxssa.BinOp:
		switch expr.Op {
		case token.ADD:
			b.trackValue(value)
		}
	}
}

// trackValue locates pointers in a value (possibly an aggregate) and tracks the
// individual pointers
func (b *builder) trackValue(value llvm.Value) {
	typ := value.Type()
	switch typ.TypeKind() {
	case llvm.PointerTypeKind:
		b.trackPointer(value)
	case llvm.StructTypeKind:
		if !typeHasPointers(typ) {
			return
		}
		numElements := typ.StructElementTypesCount()
		for i := 0; i < numElements; i++ {
			subValue := b.CreateExtractValue(value, i, "")
			b.trackValue(subValue)
		}
	case llvm.ArrayTypeKind:
		if !typeHasPointers(typ) {
			return
		}
		numElements := typ.ArrayLength()
		for i := 0; i < numElements; i++ {
			subValue := b.CreateExtractValue(value, i, "")
			b.trackValue(subValue)
		}
	}
}

// trackPointer creates a call to runtime.trackPointer, bitcasting the pointer
// first if needed. The input value must be of LLVM pointer type.
func (b *builder) trackPointer(value llvm.Value) {
	b.createRuntimeCall("trackPointer", []llvm.Value{value, b.stackChainAlloca}, "")
}

// typeHasPointers returns whether this type is a pointer or contains pointers.
// If the type is an aggregate type, it will check whether there is a pointer
// inside.
func typeHasPointers(t llvm.Type) bool {
	switch t.TypeKind() {
	case llvm.PointerTypeKind:
		return true
	case llvm.StructTypeKind:
		for _, subType := range t.StructElementTypes() {
			if typeHasPointers(subType) {
				return true
			}
		}
		return false
	case llvm.ArrayTypeKind:
		if typeHasPointers(t.ElementType()) {
			return true
		}
		return false
	default:
		return false
	}
}
