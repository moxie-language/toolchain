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
		fmt.Fprintf(os.Stderr, "usage: %s <path-to-goenv_version.so>\n", os.Args[0])
		os.Exit(1)
	}
	soPath := os.Args[1]
	if !filepath.IsAbs(soPath) {
		wd, _ := os.Getwd()
		soPath = filepath.Join(wd, soPath)
	}

	lib, err := openLib(soPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dlopen %s: %v\n", soPath, err)
		os.Exit(1)
	}

	var versionParse func(uintptr, int32, uintptr, uintptr, uintptr) int32
	purego.RegisterLibFunc(&versionParse, lib, "moxie_version_parse")

	var versionCompare func(uintptr, int32, uintptr, int32) int32
	purego.RegisterLibFunc(&versionCompare, lib, "moxie_version_compare")

	// Test Parse: "mx1.2.19"
	testParse(versionParse, "mx1.2.19", 1, 2, 19)
	testParse(versionParse, "go1.24.0", 1, 24, 0)
	testParse(versionParse, "mx1.3", 1, 3, 0)
	testParse(versionParse, "devel mx1.4.5 abc", 1, 4, 5)

	// Test Parse: invalid inputs
	testParseFail(versionParse, "v1.2.3")
	testParseFail(versionParse, "x")
	testParseFail(versionParse, "mx")

	// Test Compare
	testCompare(versionCompare, "mx1.2.19", "mx1.2.19", 0)
	testCompare(versionCompare, "mx1.2.18", "mx1.2.19", -1)
	testCompare(versionCompare, "mx1.3.0", "mx1.2.19", 1)
	testCompare(versionCompare, "mx2.0.0", "mx1.9.9", 1)
	testCompare(versionCompare, "go1.24.0", "mx1.24.0", 0)

	fmt.Println("\nAll goenv version roundtrip tests passed.")
}

func testParse(fn func(uintptr, int32, uintptr, uintptr, uintptr) int32, version string, wantMaj, wantMin, wantPat int32) {
	data := []byte(version)
	var maj, min, pat int32
	rc := fn(uintptr(unsafe.Pointer(&data[0])), int32(len(data)),
		uintptr(unsafe.Pointer(&maj)), uintptr(unsafe.Pointer(&min)), uintptr(unsafe.Pointer(&pat)))
	if rc != 0 {
		fmt.Fprintf(os.Stderr, "FAIL: parse(%q) returned %d, want 0\n", version, rc)
		os.Exit(1)
	}
	if maj != wantMaj || min != wantMin || pat != wantPat {
		fmt.Fprintf(os.Stderr, "FAIL: parse(%q) = (%d,%d,%d), want (%d,%d,%d)\n",
			version, maj, min, pat, wantMaj, wantMin, wantPat)
		os.Exit(1)
	}
	fmt.Printf("PASS: parse(%q) = (%d,%d,%d)\n", version, maj, min, pat)
}

func testParseFail(fn func(uintptr, int32, uintptr, uintptr, uintptr) int32, version string) {
	data := []byte(version)
	var maj, min, pat int32
	rc := fn(uintptr(unsafe.Pointer(&data[0])), int32(len(data)),
		uintptr(unsafe.Pointer(&maj)), uintptr(unsafe.Pointer(&min)), uintptr(unsafe.Pointer(&pat)))
	if rc == 0 {
		fmt.Fprintf(os.Stderr, "FAIL: parse(%q) succeeded, want failure\n", version)
		os.Exit(1)
	}
	fmt.Printf("PASS: parse(%q) correctly rejected\n", version)
}

func testCompare(fn func(uintptr, int32, uintptr, int32) int32, a, b string, want int32) {
	da := []byte(a)
	db := []byte(b)
	got := fn(uintptr(unsafe.Pointer(&da[0])), int32(len(da)),
		uintptr(unsafe.Pointer(&db[0])), int32(len(db)))
	if got != want {
		fmt.Fprintf(os.Stderr, "FAIL: compare(%q, %q) = %d, want %d\n", a, b, got, want)
		os.Exit(1)
	}
	fmt.Printf("PASS: compare(%q, %q) = %d\n", a, b, got)
}

func openLib(path string) (uintptr, error) {
	switch runtime.GOOS {
	case "linux", "darwin":
		return purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_GLOBAL)
	default:
		return 0, fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}
