package compiler

// This file emits the correct map intrinsics for map operations.

import (
	"go/token"

	"moxie/mxssa"
	"moxie/src/moxie"
	"moxie/typecheck"
	"tinygo.org/x/go-llvm"
)

// createMakeMap creates a new map object (runtime.hashmap) by allocating and
// Clear the given map.
func (b *builder) createMapClear(m llvm.Value) {
	b.createRuntimeCall("hashmapClear", []llvm.Value{m}, "")
}

func hashmapIsBinaryKeyTC(keyType typecheck.Type) bool {
	switch keyType := keyType.Underlying().(type) {
	case *typecheck.Basic:
		return keyType.Info()&(typecheck.IsBoolean|typecheck.IsInteger) != 0
	case *typecheck.Pointer:
		return true
	case *typecheck.Struct:
		for i := 0; i < keyType.NumFields(); i++ {
			if !hashmapIsBinaryKeyTC(keyType.Field(i).Type().Underlying()) {
				return false
			}
		}
		return true
	case *typecheck.Array:
		return hashmapIsBinaryKeyTC(keyType.Elem())
	default:
		return false
	}
}

func (b *builder) createMakeMapTC(expr *mxssa.MakeMap) (llvm.Value, error) {
	mapType := expr.Type().Underlying().(*typecheck.Map)
	keyType := mapType.Key().Underlying()
	llvmValueType := b.getLLVMTypeTC(mapType.Elem().Underlying())
	var llvmKeyType llvm.Type
	var alg uint64
	if isStringLikeTC(keyType) {
		llvmKeyType = b.getLLVMTypeTC(keyType)
		alg = uint64(moxie.HashmapAlgorithmContent)
	} else if hashmapIsBinaryKeyTC(keyType) {
		llvmKeyType = b.getLLVMTypeTC(keyType)
		alg = uint64(moxie.HashmapAlgorithmBinary)
	} else {
		llvmKeyType = b.getLLVMRuntimeType("_interface")
		alg = uint64(moxie.HashmapAlgorithmInterface)
	}
	keySize := b.targetData.TypeAllocSize(llvmKeyType)
	valueSize := b.targetData.TypeAllocSize(llvmValueType)
	llvmKeySize := llvm.ConstInt(b.uintptrType, keySize, false)
	llvmValueSize := llvm.ConstInt(b.uintptrType, valueSize, false)
	sizeHint := llvm.ConstInt(b.uintptrType, 8, false)
	algEnum := llvm.ConstInt(b.ctx.Int8Type(), alg, false)
	if expr.Reserve != nil {
		sizeHint = b.getValueTC(expr.Reserve, getPosTC(expr))
		var err error
		sizeHint, err = b.createConvertTC(expr.Reserve.Type(), typecheck.Typ[typecheck.Uint64], sizeHint, expr.Pos())
		if err != nil {
			return llvm.Value{}, err
		}
	}
	hashmap := b.createRuntimeCall("hashmapMake", []llvm.Value{llvmKeySize, llvmValueSize, sizeHint, algEnum}, "")
	return hashmap, nil
}

func (b *builder) createMapLookupTC(keyType, valueType typecheck.Type, m, key llvm.Value, commaOk bool, pos token.Pos) (llvm.Value, error) {
	llvmValueType := b.getLLVMTypeTC(valueType)
	mapValueAlloca, mapValueAllocaSize := b.createTemporaryAlloca(llvmValueType, "hashmap.value")
	mapValueSize := mapValueAllocaSize
	if mapValueSize.Type().IntTypeWidth() > b.uintptrType.IntTypeWidth() {
		mapValueSize = llvm.ConstTrunc(mapValueSize, b.uintptrType)
	}

	var commaOkValue llvm.Value
	origKeyType := keyType
	keyType = keyType.Underlying()
	if isStringLikeTC(keyType) {
		params := []llvm.Value{m, key, mapValueAlloca, mapValueSize}
		commaOkValue = b.createRuntimeCall("hashmapContentGet", params, "")
	} else if hashmapIsBinaryKeyTC(keyType) {
		mapKeyAlloca, mapKeySize := b.createTemporaryAlloca(key.Type(), "hashmap.key")
		b.CreateStore(key, mapKeyAlloca)
		b.zeroUndefBytes(b.getLLVMTypeTC(keyType), mapKeyAlloca)
		params := []llvm.Value{m, mapKeyAlloca, mapValueAlloca, mapValueSize}
		commaOkValue = b.createRuntimeCall("hashmapBinaryGet", params, "")
		b.emitLifetimeEnd(mapKeyAlloca, mapKeySize)
	} else {
		itfKey := key
		if _, ok := keyType.(*typecheck.Interface); !ok {
			itfKey = b.createMakeInterfaceTC(key, origKeyType, pos)
		}
		params := []llvm.Value{m, itfKey, mapValueAlloca, mapValueSize}
		commaOkValue = b.createRuntimeCall("hashmapInterfaceGet", params, "")
	}

	mapValue := b.CreateLoad(llvmValueType, mapValueAlloca, "")
	b.emitLifetimeEnd(mapValueAlloca, mapValueAllocaSize)

	if commaOk {
		tuple := llvm.Undef(b.ctx.StructType([]llvm.Type{llvmValueType, b.ctx.Int1Type()}, false))
		tuple = b.CreateInsertValue(tuple, mapValue, 0, "")
		tuple = b.CreateInsertValue(tuple, commaOkValue, 1, "")
		return tuple, nil
	}
	return mapValue, nil
}

func (b *builder) createMapUpdateTC(keyType typecheck.Type, m, key, value llvm.Value, pos token.Pos) {
	valueAlloca, valueSize := b.createTemporaryAlloca(value.Type(), "hashmap.value")
	b.CreateStore(value, valueAlloca)
	origKeyType := keyType
	keyType = keyType.Underlying()
	if isStringLikeTC(keyType) {
		params := []llvm.Value{m, key, valueAlloca}
		b.createRuntimeCall("hashmapContentSet", params, "")
	} else if hashmapIsBinaryKeyTC(keyType) {
		keyAlloca, keySize := b.createTemporaryAlloca(key.Type(), "hashmap.key")
		b.CreateStore(key, keyAlloca)
		b.zeroUndefBytes(b.getLLVMTypeTC(keyType), keyAlloca)
		params := []llvm.Value{m, keyAlloca, valueAlloca}
		b.createRuntimeCall("hashmapBinarySet", params, "")
		b.emitLifetimeEnd(keyAlloca, keySize)
	} else {
		itfKey := key
		if _, ok := keyType.(*typecheck.Interface); !ok {
			itfKey = b.createMakeInterfaceTC(key, origKeyType, pos)
		}
		params := []llvm.Value{m, itfKey, valueAlloca}
		b.createRuntimeCall("hashmapInterfaceSet", params, "")
	}
	b.emitLifetimeEnd(valueAlloca, valueSize)
}

func (b *builder) createMapDeleteTC(keyType typecheck.Type, m, key llvm.Value, pos token.Pos) error {
	origKeyType := keyType
	keyType = keyType.Underlying()
	if isStringLikeTC(keyType) {
		params := []llvm.Value{m, key}
		b.createRuntimeCall("hashmapContentDelete", params, "")
		return nil
	} else if hashmapIsBinaryKeyTC(keyType) {
		keyAlloca, keySize := b.createTemporaryAlloca(key.Type(), "hashmap.key")
		b.CreateStore(key, keyAlloca)
		b.zeroUndefBytes(b.getLLVMTypeTC(keyType), keyAlloca)
		params := []llvm.Value{m, keyAlloca}
		b.createRuntimeCall("hashmapBinaryDelete", params, "")
		b.emitLifetimeEnd(keyAlloca, keySize)
		return nil
	} else {
		itfKey := key
		if _, ok := keyType.(*typecheck.Interface); !ok {
			itfKey = b.createMakeInterfaceTC(key, origKeyType, pos)
		}
		params := []llvm.Value{m, itfKey}
		b.createRuntimeCall("hashmapInterfaceDelete", params, "")
		return nil
	}
}

func (b *builder) createMapIteratorNextTC(rangeVal mxssa.Value, llvmRangeVal, it llvm.Value) llvm.Value {
	keyType := rangeVal.Type().Underlying().(*typecheck.Map).Key()
	valueType := rangeVal.Type().Underlying().(*typecheck.Map).Elem()
	llvmKeyType := b.getLLVMTypeTC(keyType)
	llvmValueType := b.getLLVMTypeTC(valueType)

	isKeyStoredAsInterface := false
	if isStringLikeTC(keyType.Underlying()) {
	} else if hashmapIsBinaryKeyTC(keyType) {
	} else {
		if _, ok := keyType.Underlying().(*typecheck.Interface); !ok {
			isKeyStoredAsInterface = true
		}
	}

	llvmStoredKeyType := llvmKeyType
	if isKeyStoredAsInterface {
		llvmStoredKeyType = b.getLLVMRuntimeType("_interface")
	}

	mapKeyAlloca, mapKeySize := b.createTemporaryAlloca(llvmStoredKeyType, "range.key")
	mapValueAlloca, mapValueSize := b.createTemporaryAlloca(llvmValueType, "range.value")
	ok := b.createRuntimeCall("hashmapNext", []llvm.Value{llvmRangeVal, it, mapKeyAlloca, mapValueAlloca}, "range.next")
	mapKey := b.CreateLoad(llvmStoredKeyType, mapKeyAlloca, "")
	mapValue := b.CreateLoad(llvmValueType, mapValueAlloca, "")

	if isKeyStoredAsInterface {
		mapKey = b.extractValueFromInterface(mapKey, llvmKeyType)
	}

	b.emitLifetimeEnd(mapKeyAlloca, mapKeySize)
	b.emitLifetimeEnd(mapValueAlloca, mapValueSize)

	tuple := llvm.Undef(b.ctx.StructType([]llvm.Type{b.ctx.Int1Type(), llvmKeyType, llvmValueType}, false))
	tuple = b.CreateInsertValue(tuple, ok, 0, "")
	tuple = b.CreateInsertValue(tuple, mapKey, 1, "")
	tuple = b.CreateInsertValue(tuple, mapValue, 2, "")
	return tuple
}

func (b *builder) zeroUndefBytes(llvmType llvm.Type, ptr llvm.Value) error {
	// We know that hashmapIsBinaryKey is true, so we only have to handle those types that can show up there.
	// To zero all undefined bytes, we iterate over all the fields in the type.  For each element, compute the
	// offset of that element.  If it's Basic type, there are no internal padding bytes.  For compound types, we recurse to ensure
	// we handle nested types.  Next, we determine if there are any padding bytes before the next
	// element and zero those as well.

	zero := llvm.ConstInt(b.ctx.Int32Type(), 0, false)

	switch llvmType.TypeKind() {
	case llvm.IntegerTypeKind:
		// no padding bytes
		return nil
	case llvm.PointerTypeKind:
		// mo padding bytes
		return nil
	case llvm.ArrayTypeKind:
		llvmArrayType := llvmType
		llvmElemType := llvmType.ElementType()

		for i := 0; i < llvmArrayType.ArrayLength(); i++ {
			idx := llvm.ConstInt(b.uintptrType, uint64(i), false)
			elemPtr := b.CreateInBoundsGEP(llvmArrayType, ptr, []llvm.Value{zero, idx}, "")

			// zero any padding bytes in this element
			b.zeroUndefBytes(llvmElemType, elemPtr)
		}

	case llvm.StructTypeKind:
		llvmStructType := llvmType
		numFields := llvmStructType.StructElementTypesCount()
		llvmElementTypes := llvmStructType.StructElementTypes()

		for i := 0; i < numFields; i++ {
			idx := llvm.ConstInt(b.ctx.Int32Type(), uint64(i), false)
			elemPtr := b.CreateInBoundsGEP(llvmStructType, ptr, []llvm.Value{zero, idx}, "")

			// zero any padding bytes in this field
			llvmElemType := llvmElementTypes[i]
			b.zeroUndefBytes(llvmElemType, elemPtr)

			// zero any padding bytes before the next field, if any
			offset := b.targetData.ElementOffset(llvmStructType, i)
			storeSize := b.targetData.TypeStoreSize(llvmElemType)
			fieldEndOffset := offset + storeSize

			var nextOffset uint64
			if i < numFields-1 {
				nextOffset = b.targetData.ElementOffset(llvmStructType, i+1)
			} else {
				// Last field?  Next offset is the total size of the allocate struct.
				nextOffset = b.targetData.TypeAllocSize(llvmStructType)
			}

			if fieldEndOffset != nextOffset {
				n := llvm.ConstInt(b.uintptrType, nextOffset-fieldEndOffset, false)
				llvmStoreSize := llvm.ConstInt(b.uintptrType, storeSize, false)
				paddingStart := b.CreateInBoundsGEP(b.ctx.Int8Type(), elemPtr, []llvm.Value{llvmStoreSize}, "")
				b.createRuntimeCall("memzero", []llvm.Value{paddingStart, n}, "")
			}
		}
	}

	return nil
}
