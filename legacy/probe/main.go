package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/ebitengine/purego"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: %s <path-to-hello.so>\n", os.Args[0])
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

	var moxieAdd func(int32, int32) int32
	purego.RegisterLibFunc(&moxieAdd, lib, "moxie_add")

	var moxieMul func(int32, int32) int32
	purego.RegisterLibFunc(&moxieMul, lib, "moxie_mul")

	// test add
	got := moxieAdd(3, 4)
	if got != 7 {
		fmt.Fprintf(os.Stderr, "FAIL: moxie_add(3,4) = %d, want 7\n", got)
		os.Exit(1)
	}
	fmt.Printf("PASS: moxie_add(3,4) = %d\n", got)

	// test mul
	got = moxieMul(6, 7)
	if got != 42 {
		fmt.Fprintf(os.Stderr, "FAIL: moxie_mul(6,7) = %d, want 42\n", got)
		os.Exit(1)
	}
	fmt.Printf("PASS: moxie_mul(6,7) = %d\n", got)

	// test edge: zero
	got = moxieAdd(0, 0)
	if got != 0 {
		fmt.Fprintf(os.Stderr, "FAIL: moxie_add(0,0) = %d, want 0\n", got)
		os.Exit(1)
	}
	fmt.Printf("PASS: moxie_add(0,0) = %d\n", got)

	// test edge: negative
	got = moxieAdd(-10, 3)
	if got != -7 {
		fmt.Fprintf(os.Stderr, "FAIL: moxie_add(-10,3) = %d, want -7\n", got)
		os.Exit(1)
	}
	fmt.Printf("PASS: moxie_add(-10,3) = %d\n", got)

	fmt.Println("\nAll roundtrip tests passed.")
}

func openLib(path string) (uintptr, error) {
	switch runtime.GOOS {
	case "linux", "darwin":
		return purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_GLOBAL)
	default:
		return 0, fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}
