package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"unsafe"

	"github.com/ebitengine/purego"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: %s <path-to-mxssa.so>\n", os.Args[0])
		os.Exit(1)
	}
	soPath := os.Args[1]
	if !filepath.IsAbs(soPath) {
		wd, _ := os.Getwd()
		soPath = filepath.Join(wd, soPath)
	}

	lib, err := purego.Dlopen(soPath, purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dlopen %s: %v\n", soPath, err)
		os.Exit(1)
	}

	var newProgram func() int32
	purego.RegisterLibFunc(&newProgram, lib, "moxie_ssa_new_program")

	var buildPackage func(int32, uintptr, int32, uintptr, int32) int32
	purego.RegisterLibFunc(&buildPackage, lib, "moxie_ssa_build_package")

	var memberCount func(int32) int32
	purego.RegisterLibFunc(&memberCount, lib, "moxie_ssa_pkg_member_count")

	var memberName func(int32, int32, uintptr, int32) int32
	purego.RegisterLibFunc(&memberName, lib, "moxie_ssa_pkg_member_name")

	var memberKind func(int32, uintptr, int32) int32
	purego.RegisterLibFunc(&memberKind, lib, "moxie_ssa_member_kind")

	var funcBlockCount func(int32, uintptr, int32) int32
	purego.RegisterLibFunc(&funcBlockCount, lib, "moxie_ssa_func_block_count")

	var funcParamCount func(int32, uintptr, int32) int32
	purego.RegisterLibFunc(&funcParamCount, lib, "moxie_ssa_func_param_count")

	var funcLocalCount func(int32, uintptr, int32) int32
	purego.RegisterLibFunc(&funcLocalCount, lib, "moxie_ssa_func_local_count")

	var blockInstrCount func(int32, uintptr, int32, int32) int32
	purego.RegisterLibFunc(&blockInstrCount, lib, "moxie_ssa_block_instr_count")

	var instrString func(int32, uintptr, int32, int32, int32, uintptr, int32) int32
	purego.RegisterLibFunc(&instrString, lib, "moxie_ssa_instr_string")

	var funcType func(int32, uintptr, int32, uintptr, int32) int32
	purego.RegisterLibFunc(&funcType, lib, "moxie_ssa_func_type")

	var freePkg func(int32)
	purego.RegisterLibFunc(&freePkg, lib, "moxie_ssa_free_pkg")

	var freeProg func(int32)
	purego.RegisterLibFunc(&freeProg, lib, "moxie_ssa_free_prog")

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

	readNameStr := func(fn func(int32, uintptr, int32, uintptr, int32) int32, h int32, name string) string {
		nb := []byte(name)
		buf := make([]byte, 512)
		n := fn(h, uintptr(unsafe.Pointer(&nb[0])), int32(len(nb)),
			uintptr(unsafe.Pointer(&buf[0])), 512)
		return string(buf[:n])
	}

	src := []byte(`package mypkg

var x int32
var y string

func add(a, b int32) int32 {
	return a + b
}

func greet(name string) string {
	return "hello " | name
}

type Point struct {
	X int32
	Y int32
}

const maxSize = 100
`)
	name := []byte("mypkg")

	// Create program.
	progH := newProgram()
	fmt.Printf("new_program: handle = %d\n", progH)
	assert("program created", progH >= 0)

	// Build package.
	pkgH := buildPackage(progH,
		uintptr(unsafe.Pointer(&src[0])), int32(len(src)),
		uintptr(unsafe.Pointer(&name[0])), int32(len(name)),
	)
	fmt.Printf("build_package: handle = %d\n", pkgH)
	assert("package built", pkgH >= 0)

	if pkgH < 0 {
		fmt.Printf("%d/%d tests passed\n", pass, pass+fail)
		os.Exit(1)
	}

	// Member count: x, y, add, greet, Point, maxSize = 6 members.
	mc := memberCount(pkgH)
	fmt.Printf("member_count: %d\n", mc)
	assert(fmt.Sprintf("member count == 6, got %d", mc), mc == 6)

	// Check member names.
	expectedMembers := map[string]bool{
		"x": false, "y": false, "add": false,
		"greet": false, "Point": false, "maxSize": false,
	}
	for i := int32(0); i < mc; i++ {
		buf := make([]byte, 256)
		n := memberName(pkgH, i, uintptr(unsafe.Pointer(&buf[0])), 256)
		nm := string(buf[:n])
		if _, ok := expectedMembers[nm]; ok {
			expectedMembers[nm] = true
		} else {
			fmt.Fprintf(os.Stderr, "UNEXPECTED member[%d] = %q\n", i, nm)
		}
	}
	for nm, found := range expectedMembers {
		assert(fmt.Sprintf("member %q found", nm), found)
	}

	// Check member kinds.
	// 1=func, 2=global, 3=type, 4=const
	checkKind := func(mname string, expectedKind int32, kindName string) {
		nb := []byte(mname)
		k := memberKind(pkgH, uintptr(unsafe.Pointer(&nb[0])), int32(len(nb)))
		assert(fmt.Sprintf("%s is %s (kind=%d, got %d)", mname, kindName, expectedKind, k), k == expectedKind)
	}
	checkKind("add", 1, "function")
	checkKind("greet", 1, "function")
	checkKind("x", 2, "global")
	checkKind("y", 2, "global")
	checkKind("Point", 3, "type")
	checkKind("maxSize", 4, "const")

	// Check add function has blocks (body was built).
	addName := []byte("add")
	blocks := funcBlockCount(pkgH, uintptr(unsafe.Pointer(&addName[0])), int32(len(addName)))
	fmt.Printf("add block_count: %d\n", blocks)
	assert(fmt.Sprintf("add has blocks (got %d)", blocks), blocks > 0)

	// Check add function has 2 parameters (a, b).
	params := funcParamCount(pkgH, uintptr(unsafe.Pointer(&addName[0])), int32(len(addName)))
	fmt.Printf("add param_count: %d\n", params)
	assert(fmt.Sprintf("add has 2 params (got %d)", params), params == 2)

	// Check add function has locals (alloca for params + return).
	locals := funcLocalCount(pkgH, uintptr(unsafe.Pointer(&addName[0])), int32(len(addName)))
	fmt.Printf("add local_count: %d\n", locals)
	assert(fmt.Sprintf("add has locals (got %d)", locals), locals > 0)

	// Check entry block has instructions.
	instrs := blockInstrCount(pkgH, uintptr(unsafe.Pointer(&addName[0])), int32(len(addName)), 0)
	fmt.Printf("add block[0] instr_count: %d\n", instrs)
	assert(fmt.Sprintf("add block[0] has instrs (got %d)", instrs), instrs > 0)

	// Print all instructions for debugging.
	for bi := int32(0); bi < blocks; bi++ {
		ic := blockInstrCount(pkgH, uintptr(unsafe.Pointer(&addName[0])), int32(len(addName)), bi)
		for ii := int32(0); ii < ic; ii++ {
			buf := make([]byte, 512)
			n := instrString(pkgH,
				uintptr(unsafe.Pointer(&addName[0])), int32(len(addName)),
				bi, ii,
				uintptr(unsafe.Pointer(&buf[0])), 512)
			fmt.Printf("  add.block[%d].instr[%d] = %s\n", bi, ii, string(buf[:n]))
		}
	}

	// Check greet function.
	greetName := []byte("greet")
	gBlocks := funcBlockCount(pkgH, uintptr(unsafe.Pointer(&greetName[0])), int32(len(greetName)))
	fmt.Printf("greet block_count: %d\n", gBlocks)
	assert(fmt.Sprintf("greet has blocks (got %d)", gBlocks), gBlocks > 0)

	gParams := funcParamCount(pkgH, uintptr(unsafe.Pointer(&greetName[0])), int32(len(greetName)))
	assert(fmt.Sprintf("greet has 1 param (got %d)", gParams), gParams == 1)

	// Check func type string.
	addType := readNameStr(funcType, pkgH, "add")
	fmt.Printf("add type: %s\n", addType)
	assert("add has type string", len(addType) > 0)

	freePkg(pkgH)
	freeProg(progH)

	runtime.KeepAlive(src)
	runtime.KeepAlive(name)

	fmt.Printf("%d/%d tests passed\n", pass, pass+fail)
	if fail > 0 {
		os.Exit(1)
	}
}
