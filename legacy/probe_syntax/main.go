package main

import (
	"fmt"
	"os"
	"path/filepath"
	"unsafe"

	"github.com/ebitengine/purego"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: %s <path-to-syntax.so>\n", os.Args[0])
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

	var parseFile func(uintptr, int32) int32
	purego.RegisterLibFunc(&parseFile, lib, "moxie_parse_file")

	var parseBytes func(uintptr, int32, uintptr, int32) int32
	purego.RegisterLibFunc(&parseBytes, lib, "moxie_parse_bytes")

	var filePkgName func(int32, uintptr, int32) int32
	purego.RegisterLibFunc(&filePkgName, lib, "moxie_file_pkg_name")

	var fileDeclCount func(int32) int32
	purego.RegisterLibFunc(&fileDeclCount, lib, "moxie_file_decl_count")

	var fileDeclKind func(int32, int32) int32
	purego.RegisterLibFunc(&fileDeclKind, lib, "moxie_file_decl_kind")

	var fileImportPath func(int32, int32, uintptr, int32) int32
	purego.RegisterLibFunc(&fileImportPath, lib, "moxie_file_import_path")

	var fileFuncName func(int32, int32, uintptr, int32) int32
	purego.RegisterLibFunc(&fileFuncName, lib, "moxie_file_func_name")

	var fileFuncHasBody func(int32, int32) int32
	purego.RegisterLibFunc(&fileFuncHasBody, lib, "moxie_file_func_has_body")

	var fileFuncParamCount func(int32, int32) int32
	purego.RegisterLibFunc(&fileFuncParamCount, lib, "moxie_file_func_param_count")

	var fileFuncResultCount func(int32, int32) int32
	purego.RegisterLibFunc(&fileFuncResultCount, lib, "moxie_file_func_result_count")

	var fileFuncBodyStmtCount func(int32, int32) int32
	purego.RegisterLibFunc(&fileFuncBodyStmtCount, lib, "moxie_file_func_body_stmt_count")

	var fileFuncHasRecv func(int32, int32) int32
	purego.RegisterLibFunc(&fileFuncHasRecv, lib, "moxie_file_func_has_recv")

	var fileTypeName func(int32, int32, uintptr, int32) int32
	purego.RegisterLibFunc(&fileTypeName, lib, "moxie_file_type_name")

	var fileTypeIsAlias func(int32, int32) int32
	purego.RegisterLibFunc(&fileTypeIsAlias, lib, "moxie_file_type_is_alias")

	var fileVarNameCount func(int32, int32) int32
	purego.RegisterLibFunc(&fileVarNameCount, lib, "moxie_file_var_name_count")

	var fileConstNameCount func(int32, int32) int32
	purego.RegisterLibFunc(&fileConstNameCount, lib, "moxie_file_const_name_count")

	var fileGoVersion func(int32, uintptr, int32) int32
	purego.RegisterLibFunc(&fileGoVersion, lib, "moxie_file_go_version")

	var fileFree func(int32)
	purego.RegisterLibFunc(&fileFree, lib, "moxie_file_free")

	// Helper to read a string from a function that writes to a buffer
	readStr := func(fn func(int32, uintptr, int32) int32, h int32) string {
		buf := make([]byte, 256)
		n := fn(h, uintptr(unsafe.Pointer(&buf[0])), 256)
		return string(buf[:n])
	}
	readStr2 := func(fn func(int32, int32, uintptr, int32) int32, h, idx int32) string {
		buf := make([]byte, 256)
		n := fn(h, idx, uintptr(unsafe.Pointer(&buf[0])), 256)
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

	// Parse inline test source
	testSrc := []byte(`package testpkg

import "fmt"
import "os"

const maxSize = 1024

type Point struct {
	X, Y int32
}

type Stringer interface {
	String() string
}

var globalCounter int32

func add(a, b int32) int32 {
	return a + b
}

func (p *Point) String() string {
	return fmt.Sprintf("(%d, %d)", p.X, p.Y)
}

func main() {
	p := &Point{X: 1, Y: 2}
	fmt.Println(p.String())
	os.Exit(0)
}
`)
	testName := []byte("testdata.mx")
	h := parseBytes(
		uintptr(unsafe.Pointer(&testSrc[0])), int32(len(testSrc)),
		uintptr(unsafe.Pointer(&testName[0])), int32(len(testName)),
	)
	assert("parse returns valid handle", h >= 0)

	if h < 0 {
		fmt.Fprintf(os.Stderr, "parse failed, aborting\n")
		os.Exit(1)
	}

	// Package name
	pkgName := readStr(filePkgName, h)
	assert("pkg name is testpkg", pkgName == "testpkg")

	// Declaration count
	// testdata.mx has: 2 imports, 1 const, 2 types (Point, Stringer), 1 var, 3 funcs (add, String method, main) = 9
	declCount := fileDeclCount(h)
	assert(fmt.Sprintf("decl count is 9, got %d", declCount), declCount == 9)

	// Check decl kinds
	// Decl kinds: 1=import, 2=const, 3=type, 4=var, 5=func
	expectedKinds := []int32{1, 1, 2, 3, 3, 4, 5, 5, 5}
	for i, expected := range expectedKinds {
		kind := fileDeclKind(h, int32(i))
		assert(fmt.Sprintf("decl[%d] kind=%d, got %d", i, expected, kind), kind == expected)
	}

	// Import paths (decl 0 and 1)
	imp0 := readStr2(fileImportPath, h, 0)
	assert("import[0] path is \"fmt\", got "+imp0, imp0 == "\"fmt\"")
	imp1 := readStr2(fileImportPath, h, 1)
	assert("import[1] path is \"os\", got "+imp1, imp1 == "\"os\"")

	// Const name count (decl 2)
	constCount := fileConstNameCount(h, 2)
	assert(fmt.Sprintf("const name count=1, got %d", constCount), constCount == 1)

	// Type names (decl 3 and 4)
	typeName0 := readStr2(fileTypeName, h, 3)
	assert("type[3] name is Point, got "+typeName0, typeName0 == "Point")
	typeName1 := readStr2(fileTypeName, h, 4)
	assert("type[4] name is Stringer, got "+typeName1, typeName1 == "Stringer")

	// Type alias check
	isAlias := fileTypeIsAlias(h, 3)
	assert("Point is not alias", isAlias == 0)

	// Var name count (decl 5)
	varCount := fileVarNameCount(h, 5)
	assert(fmt.Sprintf("var name count=1, got %d", varCount), varCount == 1)

	// Function names (decl 6, 7, 8)
	fn0 := readStr2(fileFuncName, h, 6)
	assert("func[6] name is add, got "+fn0, fn0 == "add")
	fn1 := readStr2(fileFuncName, h, 7)
	assert("func[7] name is String, got "+fn1, fn1 == "String")
	fn2 := readStr2(fileFuncName, h, 8)
	assert("func[8] name is main, got "+fn2, fn2 == "main")

	// Function properties
	assert("add has body", fileFuncHasBody(h, 6) == 1)
	assert("add has 2 params", fileFuncParamCount(h, 6) == 2)
	assert("add has 1 result", fileFuncResultCount(h, 6) == 1)
	assert("add body has 1 stmt", fileFuncBodyStmtCount(h, 6) == 1)
	assert("add has no receiver", fileFuncHasRecv(h, 6) == 0)

	// Method check
	assert("String has receiver", fileFuncHasRecv(h, 7) == 1)
	assert("String has 0 params", fileFuncParamCount(h, 7) == 0)
	assert("String has 1 result", fileFuncResultCount(h, 7) == 1)

	// main
	assert("main has body", fileFuncHasBody(h, 8) == 1)
	assert("main has 0 params", fileFuncParamCount(h, 8) == 0)
	assert("main has 0 results", fileFuncResultCount(h, 8) == 0)

	// Free the handle
	fileFree(h)

	// Go version (no //go:build in testdata, should be empty)
	goVer := readStr(fileGoVersion, h)
	assert("go version is empty", goVer == "")

	fileFree(h)

	fmt.Printf("%d/%d tests passed\n", pass, pass+fail)
	if fail > 0 {
		os.Exit(1)
	}
}
