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
		fmt.Fprintf(os.Stderr, "usage: %s <path-to-typecheck.so>\n", os.Args[0])
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

	var universeOk func() int32
	purego.RegisterLibFunc(&universeOk, lib, "moxie_tc_universe_ok")

	var parseOnly func(uintptr, int32, uintptr, int32) int32
	purego.RegisterLibFunc(&parseOnly, lib, "moxie_tc_parse_only")

	var pass1Insert func(uintptr, int32, uintptr, int32) int32
	purego.RegisterLibFunc(&pass1Insert, lib, "moxie_tc_pass1_insert")

	var checkBytes func(uintptr, int32, uintptr, int32) int32
	purego.RegisterLibFunc(&checkBytes, lib, "moxie_tc_check_bytes")

	var scopeNameCount func(int32) int32
	purego.RegisterLibFunc(&scopeNameCount, lib, "moxie_tc_scope_name_count")

	var scopeTypeOf func(int32, uintptr, int32, uintptr, int32) int32
	purego.RegisterLibFunc(&scopeTypeOf, lib, "moxie_tc_scope_type_of")

	var pkgName func(int32, uintptr, int32) int32
	purego.RegisterLibFunc(&pkgName, lib, "moxie_tc_pkg_name")

	var pkgPath func(int32, uintptr, int32) int32
	purego.RegisterLibFunc(&pkgPath, lib, "moxie_tc_pkg_path")

	var tcFree func(int32)
	purego.RegisterLibFunc(&tcFree, lib, "moxie_tc_free")

	var scopeName func(int32, int32, uintptr, int32) int32
	purego.RegisterLibFunc(&scopeName, lib, "moxie_tc_scope_name")

	readStr := func(fn func(int32, uintptr, int32) int32, h int32) string {
		buf := make([]byte, 256)
		n := fn(h, uintptr(unsafe.Pointer(&buf[0])), 256)
		return string(buf[:n])
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

	src1 := []byte(`package mypkg

var x int32

func add(a, b int32) int32 {
	return a + b
}

type Point struct {
	X int32
	Y int32
}
`)
	name1 := []byte("mypkg")

	uLen := universeOk()
	fmt.Printf("universe: %d names\n", uLen)
	assert("universe initialized", uLen > 0)

	h := checkBytes(
		uintptr(unsafe.Pointer(&src1[0])), int32(len(src1)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
	)
	fmt.Printf("checkBytes: handle = %d\n", h)
	assert("check returns valid handle", h >= 0)

	if h < 0 {
		fmt.Printf("%d/%d tests passed\n", pass, pass+fail)
		os.Exit(1)
	}

	pName := readStr(pkgName, h)
	assert("pkg name is mypkg, got "+pName, pName == "mypkg")

	pPath := readStr(pkgPath, h)
	assert("pkg path is mypkg, got "+pPath, pPath == "mypkg")

	nameCount := scopeNameCount(h)
	assert(fmt.Sprintf("scope has names (count=%d)", nameCount), nameCount == 3)

	// Verify Names() iteration returns all 3 unique keys
	expectedNames := map[string]bool{"x": false, "add": false, "Point": false}
	for i := int32(0); i < nameCount; i++ {
		nbuf := make([]byte, 256)
		nn := scopeName(h, i, uintptr(unsafe.Pointer(&nbuf[0])), 256)
		nm := string(nbuf[:nn])
		if _, ok := expectedNames[nm]; ok {
			expectedNames[nm] = true
		} else {
			fmt.Fprintf(os.Stderr, "UNEXPECTED name[%d] = %q\n", i, nm)
		}
	}
	for nm, found := range expectedNames {
		assert(fmt.Sprintf("name %q found via Names() iteration", nm), found)
	}

	xName := []byte("x")
	buf := make([]byte, 256)
	n := scopeTypeOf(h, uintptr(unsafe.Pointer(&xName[0])), int32(len(xName)),
		uintptr(unsafe.Pointer(&buf[0])), 256)
	xType := string(buf[:n])
	assert("x type is int32, got "+xType, xType == "int32")

	addName := []byte("add")
	n = scopeTypeOf(h, uintptr(unsafe.Pointer(&addName[0])), int32(len(addName)),
		uintptr(unsafe.Pointer(&buf[0])), 256)
	addType := string(buf[:n])
	assert("add has a type string, got "+addType, len(addType) > 0)

	ptName := []byte("Point")
	n = scopeTypeOf(h, uintptr(unsafe.Pointer(&ptName[0])), int32(len(ptName)),
		uintptr(unsafe.Pointer(&buf[0])), 256)
	ptType := string(buf[:n])
	assert("Point has a type string, got "+ptType, len(ptType) > 0)

	tcFree(h)

	po := parseOnly(
		uintptr(unsafe.Pointer(&src1[0])), int32(len(src1)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
	)
	fmt.Printf("parseOnly: %d decls\n", po)
	assert("parse works", po >= 3)

	p1i := pass1Insert(
		uintptr(unsafe.Pointer(&src1[0])), int32(len(src1)),
		uintptr(unsafe.Pointer(&name1[0])), int32(len(name1)),
	)
	fmt.Printf("pass1Insert: %d names\n", p1i)
	assert("pass1 insert works", p1i == 3)

	runtime.KeepAlive(src1)
	runtime.KeepAlive(name1)

	fmt.Printf("%d/%d tests passed\n", pass, pass+fail)
	if fail > 0 {
		os.Exit(1)
	}
}
