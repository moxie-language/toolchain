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
		fmt.Fprintf(os.Stderr, "usage: %s <path-to-loader.so>\n", os.Args[0])
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

	var loaderLoad func(uintptr, int32, uintptr, int32, uintptr, int32) int32
	purego.RegisterLibFunc(&loaderLoad, lib, "moxie_loader_load")

	var pkgCount func(int32) int32
	purego.RegisterLibFunc(&pkgCount, lib, "moxie_loader_pkg_count")

	var modPath func(int32, uintptr, int32) int32
	purego.RegisterLibFunc(&modPath, lib, "moxie_loader_mod_path")

	var pkgPath func(int32, int32, uintptr, int32) int32
	purego.RegisterLibFunc(&pkgPath, lib, "moxie_loader_pkg_path")

	var pkgDir func(int32, int32, uintptr, int32) int32
	purego.RegisterLibFunc(&pkgDir, lib, "moxie_loader_pkg_dir")

	var pkgName func(int32, int32, uintptr, int32) int32
	purego.RegisterLibFunc(&pkgName, lib, "moxie_loader_pkg_name")

	var pkgFileCount func(int32, int32) int32
	purego.RegisterLibFunc(&pkgFileCount, lib, "moxie_loader_pkg_file_count")

	var pkgImportCount func(int32, int32) int32
	purego.RegisterLibFunc(&pkgImportCount, lib, "moxie_loader_pkg_import_count")

	var pkgImport func(int32, int32, int32, uintptr, int32) int32
	purego.RegisterLibFunc(&pkgImport, lib, "moxie_loader_pkg_import")

	var loaderFree func(int32)
	purego.RegisterLibFunc(&loaderFree, lib, "moxie_loader_free")

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

	readStr := func(fn func(int32, uintptr, int32) int32, h int32) string {
		buf := make([]byte, 512)
		n := fn(h, uintptr(unsafe.Pointer(&buf[0])), 512)
		return string(buf[:n])
	}

	readPkgStr := func(fn func(int32, int32, uintptr, int32) int32, h int32, idx int32) string {
		buf := make([]byte, 512)
		n := fn(h, idx, uintptr(unsafe.Pointer(&buf[0])), 512)
		return string(buf[:n])
	}

	readImport := func(h int32, idx int32, impIdx int32) string {
		buf := make([]byte, 512)
		n := pkgImport(h, idx, impIdx, uintptr(unsafe.Pointer(&buf[0])), 512)
		return string(buf[:n])
	}

	// Create a temporary multi-package project.
	tmpDir, err := os.MkdirTemp("", "loader-probe-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "mktempdir: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmpDir)

	// Write moxie.mod.
	os.WriteFile(filepath.Join(tmpDir, "moxie.mod"), []byte("module example.com/testpkg\n"), 0644)

	// Write main package.
	os.WriteFile(filepath.Join(tmpDir, "main.mx"), []byte(`package main

import (
	"fmt"
	"example.com/testpkg/util"
)

func main() {
	fmt.Println(util.Hello())
}
`), 0644)

	// Write util subpackage.
	os.MkdirAll(filepath.Join(tmpDir, "util"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "util", "util.mx"), []byte(`package util

import "bytes"

func Hello() string {
	return bytes.Join([]string{"hello", "world"}, " ")
}
`), 0644)

	// Find the Moxie GOROOT.
	moxieRoot := os.Getenv("MOXIEROOT")
	if moxieRoot == "" {
		// Try relative to this binary's location.
		exe, _ := os.Executable()
		moxieRoot = filepath.Dir(filepath.Dir(filepath.Dir(exe)))
	}
	goroot := filepath.Join(moxieRoot, "lib", "moxie")
	// If that doesn't exist, try the synthetic goroot path.
	if _, err := os.Stat(goroot); err != nil {
		goroot = ""
	}

	// Load the project.
	rootDirB := []byte(tmpDir)
	inputPkgB := []byte(".")
	gorootB := []byte(goroot)
	if len(gorootB) == 0 {
		gorootB = []byte{0}
	}

	h := loaderLoad(
		uintptr(unsafe.Pointer(&rootDirB[0])), int32(len(rootDirB)),
		uintptr(unsafe.Pointer(&inputPkgB[0])), int32(len(inputPkgB)),
		uintptr(unsafe.Pointer(&gorootB[0])), int32(len(goroot)),
	)
	fmt.Printf("loader_load: handle = %d\n", h)
	assert("loader returns valid handle", h >= 0)

	if h < 0 {
		fmt.Printf("%d/%d tests passed\n", pass, pass+fail)
		os.Exit(1)
	}

	// Check module path.
	mp := readStr(modPath, h)
	fmt.Printf("mod_path: %s\n", mp)
	assert("module path is example.com/testpkg", mp == "example.com/testpkg")

	// Check package count.
	count := pkgCount(h)
	fmt.Printf("pkg_count: %d\n", count)
	// Should have at least: runtime, moxie, fmt, bytes, util, main (exact count depends on stdlib resolution)
	assert(fmt.Sprintf("pkg_count >= 2, got %d", count), count >= 2)

	// Print all packages.
	for i := int32(0); i < count; i++ {
		p := readPkgStr(pkgPath, h, i)
		n := readPkgStr(pkgName, h, i)
		d := readPkgStr(pkgDir, h, i)
		fc := pkgFileCount(h, i)
		ic := pkgImportCount(h, i)
		fmt.Printf("  [%d] path=%s name=%s dir=%s files=%d imports=%d\n", i, p, n, d, fc, ic)

		// Print imports.
		for j := int32(0); j < ic; j++ {
			imp := readImport(h, i, j)
			fmt.Printf("       import: %s\n", imp)
		}
	}

	// Find the util package and main package in the list.
	foundUtil := false
	foundMain := false
	utilIdx := int32(-1)
	mainIdx := int32(-1)
	for i := int32(0); i < count; i++ {
		p := readPkgStr(pkgPath, h, i)
		if p == "example.com/testpkg/util" {
			foundUtil = true
			utilIdx = i
		}
		if p == "example.com/testpkg" {
			foundMain = true
			mainIdx = i
		}
	}
	assert("util package found", foundUtil)
	assert("main package found", foundMain)

	// util should come before main in dependency order.
	if foundUtil && foundMain {
		assert("util before main in dep order", utilIdx < mainIdx)
	}

	// util should have 1 file.
	if foundUtil {
		fc := pkgFileCount(h, utilIdx)
		assert(fmt.Sprintf("util has 1 file, got %d", fc), fc == 1)
	}

	// main should have 1 file.
	if foundMain {
		fc := pkgFileCount(h, mainIdx)
		assert(fmt.Sprintf("main has 1 file, got %d", fc), fc == 1)
	}

	// main should import util and fmt.
	if foundMain {
		ic := pkgImportCount(h, mainIdx)
		mainImports := make(map[string]bool)
		for j := int32(0); j < ic; j++ {
			imp := readImport(h, mainIdx, j)
			mainImports[imp] = true
		}
		assert("main imports fmt", mainImports["fmt"])
		assert("main imports example.com/testpkg/util", mainImports["example.com/testpkg/util"])
	}

	// util should import bytes.
	if foundUtil {
		ic := pkgImportCount(h, utilIdx)
		utilImports := make(map[string]bool)
		for j := int32(0); j < ic; j++ {
			imp := readImport(h, utilIdx, j)
			utilImports[imp] = true
		}
		assert("util imports bytes", utilImports["bytes"])
	}

	loaderFree(h)

	runtime.KeepAlive(rootDirB)
	runtime.KeepAlive(inputPkgB)
	runtime.KeepAlive(gorootB)

	fmt.Printf("%d/%d tests passed\n", pass, pass+fail)
	if fail > 0 {
		os.Exit(1)
	}
}
