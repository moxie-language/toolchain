package main

import (
	"fmt"
	"os"
	"strings"

	llvm "bootstrap/llvmpure"
)

func main() {
	llvmPath := "/usr/lib/libLLVM-19.so"
	gluePath := ""

	if len(os.Args) >= 2 {
		gluePath = os.Args[1]
	}

	if err := llvm.Init(llvmPath, gluePath); err != nil {
		fmt.Fprintf(os.Stderr, "init: %v\n", err)
		os.Exit(1)
	}

	pass := 0
	fail := 0
	assert := func(name string, cond bool) {
		if cond {
			pass++
		} else {
			fail++
			fmt.Fprintf(os.Stderr, "FAIL: %s\n", name)
		}
	}

	// 6A: Context + Module + Types

	ctx := llvm.NewContext()
	assert("context not nil", !ctx.IsNil())

	mod := ctx.NewModule("testmod")
	assert("module not nil", !mod.IsNil())

	mod.SetTarget("x86_64-unknown-linux-musl")
	assert("target roundtrip", mod.Target() == "x86_64-unknown-linux-musl")

	mod.SetDataLayout("e-m:e-p270:32:32-p271:32:32-p272:64:64-i64:64-f80:128-n8:16:32:64-S128")
	assert("datalayout set", mod.DataLayout() != "")

	modCtx := mod.Context()
	assert("module context matches", !modCtx.IsNil())

	// Integer types
	i1 := ctx.Int1Type()
	assert("i1 kind", i1.TypeKind() == llvm.IntegerTypeKind)
	assert("i1 width", i1.IntTypeWidth() == 1)

	i8 := ctx.Int8Type()
	assert("i8 width", i8.IntTypeWidth() == 8)

	i16 := ctx.Int16Type()
	assert("i16 width", i16.IntTypeWidth() == 16)

	i32 := ctx.Int32Type()
	assert("i32 width", i32.IntTypeWidth() == 32)

	i64 := ctx.Int64Type()
	assert("i64 width", i64.IntTypeWidth() == 64)

	i128 := ctx.IntType(128)
	assert("i128 width", i128.IntTypeWidth() == 128)

	// Float types
	f32 := ctx.FloatType()
	assert("float kind", f32.TypeKind() == llvm.FloatTypeKind)

	f64 := ctx.DoubleType()
	assert("double kind", f64.TypeKind() == llvm.DoubleTypeKind)

	// Void type
	voidTy := ctx.VoidType()
	assert("void kind", voidTy.TypeKind() == llvm.VoidTypeKind)

	// Function type: i32(i32, i32)
	fnType := llvm.FunctionType(i32, []llvm.Type{i32, i32}, false)
	assert("func kind", fnType.TypeKind() == llvm.FunctionTypeKind)
	assert("func not vararg", !fnType.IsFunctionVarArg())
	assert("func return type", fnType.ReturnType().IntTypeWidth() == 32)
	assert("func param count", fnType.ParamTypesCount() == 2)
	params := fnType.ParamTypes()
	assert("func param[0] width", params[0].IntTypeWidth() == 32)
	assert("func param[1] width", params[1].IntTypeWidth() == 32)

	// Vararg function type
	vaFnType := llvm.FunctionType(i32, []llvm.Type{i32}, true)
	assert("vararg func", vaFnType.IsFunctionVarArg())

	// Struct type
	structTy := ctx.StructType([]llvm.Type{i32, i64}, false)
	assert("struct kind", structTy.TypeKind() == llvm.StructTypeKind)
	assert("struct not packed", !structTy.IsStructPacked())
	assert("struct elem count", structTy.StructElementTypesCount() == 2)
	elems := structTy.StructElementTypes()
	assert("struct elem[0]", elems[0].IntTypeWidth() == 32)
	assert("struct elem[1]", elems[1].IntTypeWidth() == 64)

	// Packed struct
	packedStruct := ctx.StructType([]llvm.Type{i8, i32}, true)
	assert("packed struct", packedStruct.IsStructPacked())

	// Named struct
	namedStruct := ctx.StructCreateNamed("MyStruct")
	assert("named struct name", namedStruct.StructName() == "MyStruct")
	namedStruct.StructSetBody([]llvm.Type{i32, i64, i8}, false)
	assert("named struct body count", namedStruct.StructElementTypesCount() == 3)

	// Array type
	arrTy := llvm.ArrayType(i32, 10)
	assert("array kind", arrTy.TypeKind() == llvm.ArrayTypeKind)
	assert("array length", arrTy.ArrayLength() == 10)
	assert("array elem", arrTy.ElementType().IntTypeWidth() == 32)

	// Pointer type
	ptrTy := llvm.PointerType(i32, 0)
	assert("pointer kind", ptrTy.TypeKind() == llvm.PointerTypeKind)
	assert("pointer addrspace", ptrTy.PointerAddressSpace() == 0)

	// Vector type
	vecTy := llvm.VectorType(i32, 4)
	assert("vector kind", vecTy.TypeKind() == llvm.VectorTypeKind)
	assert("vector size", vecTy.VectorSize() == 4)

	// 6B: Builder - build add(a,b) function

	addFnType := llvm.FunctionType(i32, []llvm.Type{i32, i32}, false)
	addFn := mod.AddFunction("add", addFnType)
	assert("function added", !addFn.IsNil())
	assert("function name", addFn.Name() == "add")
	assert("function param count", addFn.ParamsCount() == 2)

	entry := ctx.AddBasicBlock(addFn, "entry")
	assert("basic block added", !entry.IsNil())

	builder := ctx.NewBuilder()
	builder.SetInsertPointAtEnd(entry)

	a := addFn.Param(0)
	b := addFn.Param(1)
	a.SetName("a")
	b.SetName("b")
	assert("param a name", a.Name() == "a")
	assert("param b name", b.Name() == "b")

	sum := builder.CreateAdd(a, b, "sum")
	assert("add instruction", !sum.IsNil())

	builder.CreateRet(sum)

	// Verify IR string contains expected content
	ir := mod.String()
	assert("IR contains add function", strings.Contains(ir, "define i32 @add"))
	assert("IR contains add instruction", strings.Contains(ir, "add i32"))
	assert("IR contains ret", strings.Contains(ir, "ret i32"))
	fmt.Printf("Generated IR:\n%s\n", ir)

	// 6C: Module inspection

	lookedUp := mod.NamedFunction("add")
	assert("named function lookup", !lookedUp.IsNil())
	assert("looked up name matches", lookedUp.Name() == "add")

	// Iterate functions
	first := mod.FirstFunction()
	assert("first function", first.Name() == "add")

	// 6A continued: Constants

	c42 := llvm.ConstInt(i32, 42, false)
	assert("const int not nil", !c42.IsNil())
	assert("const int is constant", c42.IsConstant())
	assert("const int value", c42.ZExtValue() == 42)

	cnull := llvm.ConstNull(i32)
	assert("const null is null", cnull.IsNull())

	cundef := llvm.Undef(i32)
	assert("undef is undef", cundef.IsUndef())

	// Global variable
	gv := mod.AddGlobal(i32, "myGlobal")
	assert("global added", !gv.IsNil())
	gv.SetInitializer(c42)
	gv.SetGlobalConstant(true)
	assert("global is constant", gv.IsGlobalConstant())
	gv.SetLinkage(llvm.InternalLinkage)

	namedGlobal := mod.NamedGlobal("myGlobal")
	assert("named global lookup", !namedGlobal.IsNil())

	// Verify module
	err := mod.VerifyModule()
	assert("module verifies", err == nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "verify: %v\n", err)
	}

	// Attributes
	noinlineKind := llvm.AttributeKindID("noinline")
	assert("noinline attr kind > 0", noinlineKind > 0)
	noinlineAttr := ctx.CreateEnumAttribute(noinlineKind, 0)
	assert("enum attr not nil", !noinlineAttr.IsNil())

	// MDKindID
	dbgKind := ctx.MDKindID("dbg")
	assert("dbg md kind >= 0", dbgKind >= 0)

	// Basic block operations
	bbCount := addFn.BasicBlocksCount()
	assert("add has 1 basic block", bbCount == 1)

	entryBB := addFn.EntryBasicBlock()
	assert("entry block not nil", !entryBB.IsNil())

	firstInstr := entryBB.FirstInstruction()
	assert("first instr not nil", !firstInstr.IsNil())

	lastInstr := entryBB.LastInstruction()
	assert("last instr is terminator", !lastInstr.IsATerminatorInst().IsNil())

	// Builder - more complex: if/then/else
	selFnType := llvm.FunctionType(i32, []llvm.Type{i1, i32, i32}, false)
	selFn := mod.AddFunction("myselect", selFnType)
	selEntry := ctx.AddBasicBlock(selFn, "entry")
	builder.SetInsertPointAtEnd(selEntry)
	sel := builder.CreateSelect(selFn.Param(0), selFn.Param(1), selFn.Param(2), "sel")
	builder.CreateRet(sel)

	err = mod.VerifyModule()
	assert("module with select verifies", err == nil)

	// Struct GEP
	structPtrFnType := llvm.FunctionType(i32, []llvm.Type{llvm.PointerType(structTy, 0)}, false)
	gepFn := mod.AddFunction("getfield", structPtrFnType)
	gepEntry := ctx.AddBasicBlock(gepFn, "entry")
	builder.SetInsertPointAtEnd(gepEntry)
	gep := builder.CreateStructGEP(structTy, gepFn.Param(0), 0, "field0ptr")
	loaded := builder.CreateLoad(i32, gep, "field0")
	builder.CreateRet(loaded)

	err = mod.VerifyModule()
	assert("module with GEP verifies", err == nil)

	// Cleanup first module
	builder.Dispose()
	mod.Dispose()
	ctx.Dispose()

	// ---- Phase 6D: Target machine ----

	llvm.InitializeAllTargetInfos()
	llvm.InitializeAllTargets()
	llvm.InitializeAllTargetMCs()
	llvm.InitializeAllAsmParsers()
	llvm.InitializeAllAsmPrinters()

	defTriple := llvm.DefaultTargetTriple()
	assert("default triple not empty", defTriple != "")
	fmt.Printf("default triple: %s\n", defTriple)

	target, err := llvm.GetTargetFromTriple("x86_64-unknown-linux-musl")
	assert("target from triple", err == nil)
	assert("target not nil", !target.IsNil())
	fmt.Printf("target name: %s\n", target.Name())
	assert("target name not empty", target.Name() != "")

	tm := target.CreateTargetMachine("x86_64-unknown-linux-musl", "x86-64", "",
		llvm.CodeGenLevelDefault, llvm.RelocPIC, llvm.CodeModelDefault)
	assert("target machine not nil", !tm.IsNil())

	tmTriple := tm.Triple()
	assert("tm triple", tmTriple == "x86_64-unknown-linux-musl")

	td := tm.CreateTargetData()
	assert("target data not nil", !td.IsNil())
	assert("pointer size 8", td.PointerSize() == 8)

	tdStr := td.String()
	assert("target data string not empty", tdStr != "")
	fmt.Printf("target data: %s\n", tdStr)

	// Type size queries
	ctx2 := llvm.NewContext()
	i32td := ctx2.Int32Type()
	i64td := ctx2.Int64Type()
	assert("i32 size 32 bits", td.TypeSizeInBits(i32td) == 32)
	assert("i64 size 64 bits", td.TypeSizeInBits(i64td) == 64)
	assert("i32 store size 4", td.TypeStoreSize(i32td) == 4)
	assert("i64 alloc size 8", td.TypeAllocSize(i64td) == 8)

	// Struct layout queries
	structTd := ctx2.StructType([]llvm.Type{i32td, i64td}, false)
	structSize := td.TypeAllocSize(structTd)
	assert("struct size >= 12", structSize >= 12)
	offset1 := td.ElementOffset(structTd, 1)
	assert("struct elem 1 offset >= 4", offset1 >= 4)
	fmt.Printf("struct{i32,i64} size=%d, offset[1]=%d\n", structSize, offset1)

	// Emit object code to memory buffer
	mod2 := ctx2.NewModule("emitmod")
	mod2.SetTarget("x86_64-unknown-linux-musl")
	mod2.SetDataLayout(tdStr)
	addFnType2 := llvm.FunctionType(i32td, []llvm.Type{i32td, i32td}, false)
	addFn2 := mod2.AddFunction("testadd", addFnType2)
	entry2 := ctx2.AddBasicBlock(addFn2, "entry")
	b2 := ctx2.NewBuilder()
	b2.SetInsertPointAtEnd(entry2)
	sum2 := b2.CreateAdd(addFn2.Param(0), addFn2.Param(1), "sum")
	b2.CreateRet(sum2)

	mb, emitErr := tm.EmitToMemoryBuffer(mod2, llvm.ObjectFile)
	assert("emit to membuf", emitErr == nil)
	if emitErr != nil {
		fmt.Fprintf(os.Stderr, "emit: %v\n", emitErr)
	} else {
		objBytes := mb.Bytes()
		assert("object code > 0 bytes", len(objBytes) > 0)
		// ELF magic: 0x7f 'E' 'L' 'F'
		assert("ELF magic", len(objBytes) >= 4 && objBytes[0] == 0x7f && objBytes[1] == 'E' && objBytes[2] == 'L' && objBytes[3] == 'F')
		fmt.Printf("emitted %d bytes of ELF object code\n", len(objBytes))
		mb.Dispose()
	}

	// Bitcode roundtrip
	bcBuf := llvm.WriteBitcodeToMemoryBuffer(mod2)
	assert("bitcode buf not nil", !bcBuf.IsNil())
	bcBytes := bcBuf.Bytes()
	assert("bitcode > 0 bytes", len(bcBytes) > 0)
	// Bitcode magic: 'BC' 0xc0 0xde
	assert("bitcode magic", len(bcBytes) >= 4 && bcBytes[0] == 'B' && bcBytes[1] == 'C')
	fmt.Printf("bitcode: %d bytes\n", len(bcBytes))
	bcBuf.Dispose()

	b2.Dispose()
	mod2.Dispose()

	// ---- Phase 6E: Debug info ----

	mod3 := ctx2.NewModule("debugmod")
	mod3.SetTarget("x86_64-unknown-linux-musl")
	mod3.SetDataLayout(tdStr)

	dib := llvm.NewDIBuilder(mod3)
	assert("dibuilder not nil", dib != nil)

	cu := dib.CreateCompileUnit(llvm.DICompileUnit{
		Language: llvm.DW_LANG_Go,
		File:     "test.go",
		Dir:      "/tmp",
		Producer: "moxie",
	})
	assert("compile unit not nil", !cu.IsNil())

	diFile := dib.CreateFile("test.go", "/tmp")
	assert("di file not nil", !diFile.IsNil())

	diInt := dib.CreateBasicType(llvm.DIBasicType{
		Name:       "int32",
		SizeInBits: 32,
		Encoding:   llvm.DW_ATE_signed,
	})
	assert("di basic type not nil", !diInt.IsNil())

	diPtr := dib.CreatePointerType(llvm.DIPointerType{
		Pointee:    diInt,
		SizeInBits: 64,
		Name:       "*int32",
	})
	assert("di pointer type not nil", !diPtr.IsNil())

	diFnType := dib.CreateSubroutineType(llvm.DISubroutineType{
		Parameters: []llvm.Metadata{diInt, diInt, diInt},
	})
	assert("di subroutine type not nil", !diFnType.IsNil())

	diFn := dib.CreateFunction(diFile, llvm.DIFunction{
		Name:         "myfunc",
		LinkageName:  "myfunc",
		File:         diFile,
		Line:         10,
		Type:         diFnType,
		LocalToUnit:  true,
		IsDefinition: true,
		ScopeLine:    10,
	})
	assert("di function not nil", !diFn.IsNil())

	// Create a function with debug info attached (needs a body for verifier)
	diFnLLVM := mod3.AddFunction("myfunc", llvm.FunctionType(i32td, []llvm.Type{i32td, i32td}, false))
	diFnLLVM.SetSubprogram(diFn)
	sp := diFnLLVM.Subprogram()
	assert("subprogram roundtrip", !sp.IsNil())
	b3 := ctx2.NewBuilder()
	diEntry := ctx2.AddBasicBlock(diFnLLVM, "entry")
	b3.SetInsertPointAtEnd(diEntry)
	diSum := b3.CreateAdd(diFnLLVM.Param(0), diFnLLVM.Param(1), "sum")
	b3.CreateRet(diSum)
	b3.Dispose()

	dib.Finalize()

	err = mod3.VerifyModule()
	assert("debug module verifies", err == nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "debug verify: %v\n", err)
	}

	dib.Destroy()
	mod3.Dispose()

	td.Dispose()
	tm.Dispose()
	ctx2.Dispose()

	fmt.Printf("%d/%d tests passed\n", pass, pass+fail)
	if fail > 0 {
		os.Exit(1)
	}
}
