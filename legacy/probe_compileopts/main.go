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
		fmt.Fprintf(os.Stderr, "usage: %s <path-to-compileopts.so>\n", os.Args[0])
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

	var targetLoad func(uintptr, int32, uintptr, int32, int32) int32
	purego.RegisterLibFunc(&targetLoad, lib, "moxie_target_load")

	var targetTriple func(int32, uintptr, int32) int32
	purego.RegisterLibFunc(&targetTriple, lib, "moxie_target_triple")

	var targetCPU func(int32, uintptr, int32) int32
	purego.RegisterLibFunc(&targetCPU, lib, "moxie_target_cpu")

	var targetFeatures func(int32, uintptr, int32) int32
	purego.RegisterLibFunc(&targetFeatures, lib, "moxie_target_features")

	var targetGC func(int32, uintptr, int32) int32
	purego.RegisterLibFunc(&targetGC, lib, "moxie_target_gc")

	var targetLinker func(int32, uintptr, int32) int32
	purego.RegisterLibFunc(&targetLinker, lib, "moxie_target_linker")

	var targetLibc func(int32, uintptr, int32) int32
	purego.RegisterLibFunc(&targetLibc, lib, "moxie_target_libc")

	var targetStackSize func(int32) int64
	purego.RegisterLibFunc(&targetStackSize, lib, "moxie_target_stack_size")

	var targetLdflagCount func(int32) int32
	purego.RegisterLibFunc(&targetLdflagCount, lib, "moxie_target_ldflag_count")

	var targetLdflag func(int32, int32, uintptr, int32) int32
	purego.RegisterLibFunc(&targetLdflag, lib, "moxie_target_ldflag")

	var targetExtraFileCount func(int32) int32
	purego.RegisterLibFunc(&targetExtraFileCount, lib, "moxie_target_extra_file_count")

	var targetExtraFile func(int32, int32, uintptr, int32) int32
	purego.RegisterLibFunc(&targetExtraFile, lib, "moxie_target_extra_file")

	var targetBuildTagCount func(int32) int32
	purego.RegisterLibFunc(&targetBuildTagCount, lib, "moxie_target_build_tag_count")

	var targetBuildTag func(int32, int32, uintptr, int32) int32
	purego.RegisterLibFunc(&targetBuildTag, lib, "moxie_target_build_tag")

	var targetOptLevel func(uintptr, int32, uintptr, uintptr) int32
	purego.RegisterLibFunc(&targetOptLevel, lib, "moxie_target_opt_level")

	var targetCanonicalArch func(uintptr, int32, uintptr, int32) int32
	purego.RegisterLibFunc(&targetCanonicalArch, lib, "moxie_target_canonical_arch")

	var targetFree func(int32)
	purego.RegisterLibFunc(&targetFree, lib, "moxie_target_free")

	// Test linux/amd64
	h := loadTarget(targetLoad, "linux", "amd64", 0)
	expectS(targetTriple, h, "x86_64-unknown-linux-musleabihf", "linux/amd64 triple")
	expectS(targetCPU, h, "x86-64", "linux/amd64 cpu")
	expectS(targetGC, h, "dealloc", "linux/amd64 gc")
	expectS(targetLinker, h, "ld.lld", "linux/amd64 linker")
	expectS(targetLibc, h, "musl", "linux/amd64 libc")
	expectI64(targetStackSize(h), 65536, "linux/amd64 stack_size")
	expectContainsFile(targetExtraFile, targetExtraFileCount, h, "src/runtime/asm_amd64.S", "linux/amd64 asm file")
	targetFree(h)

	// Test darwin/arm64
	h = loadTarget(targetLoad, "darwin", "arm64", 0)
	expectS(targetTriple, h, "arm64-apple-macosx11.0.0", "darwin/arm64 triple")
	expectS(targetCPU, h, "generic", "darwin/arm64 cpu")
	expectS(targetGC, h, "dealloc", "darwin/arm64 gc")
	expectS(targetLibc, h, "darwin-libSystem", "darwin/arm64 libc")
	targetFree(h)

	// Test js/wasm
	h = loadTarget(targetLoad, "js", "wasm", 0)
	expectS(targetTriple, h, "wasm32-unknown-js", "js/wasm triple")
	expectS(targetCPU, h, "generic", "js/wasm cpu")
	expectS(targetGC, h, "leaking", "js/wasm gc")
	expectS(targetLinker, h, "wasm-ld", "js/wasm linker")
	targetFree(h)

	// Test c-shared mode adds --shared ldflag
	h = loadTarget(targetLoad, "linux", "amd64", 1)
	expectContainsLdflag(targetLdflag, targetLdflagCount, h, "--shared", "c-shared --shared ldflag")
	targetFree(h)

	// Test opt levels
	testOptLevel(targetOptLevel, "0", 0, 0)
	testOptLevel(targetOptLevel, "1", 1, 0)
	testOptLevel(targetOptLevel, "2", 2, 0)
	testOptLevel(targetOptLevel, "s", 2, 1)
	testOptLevel(targetOptLevel, "z", 2, 2)
	testOptLevel(targetOptLevel, "none", 0, 0)

	// Test canonical arch
	testCanonicalArch(targetCanonicalArch, "x86_64-unknown-linux-musleabihf", "x86_64")
	testCanonicalArch(targetCanonicalArch, "arm64-apple-macosx11.0.0", "aarch64")
	testCanonicalArch(targetCanonicalArch, "wasm32-unknown-js", "wasm32")

	// Test invalid inputs
	goos := []byte("windows")
	goarch := []byte("amd64")
	bad := targetLoad(uintptr(unsafe.Pointer(&goos[0])), int32(len(goos)),
		uintptr(unsafe.Pointer(&goarch[0])), int32(len(goarch)), 0)
	if bad != -1 {
		fmt.Fprintf(os.Stderr, "FAIL: windows/amd64 should fail\n")
		os.Exit(1)
	}
	fmt.Println("PASS: windows/amd64 correctly rejected")

	fmt.Println("\nAll compileopts roundtrip tests passed.")
}

func loadTarget(fn func(uintptr, int32, uintptr, int32, int32) int32, goos, goarch string, buildMode int32) int32 {
	g := []byte(goos)
	a := []byte(goarch)
	h := fn(uintptr(unsafe.Pointer(&g[0])), int32(len(g)),
		uintptr(unsafe.Pointer(&a[0])), int32(len(a)), buildMode)
	if h < 0 {
		fmt.Fprintf(os.Stderr, "FAIL: target_load(%s, %s) returned %d\n", goos, goarch, h)
		os.Exit(1)
	}
	fmt.Printf("PASS: target_load(%s/%s) = handle %d\n", goos, goarch, h)
	return h
}

func getString(fn func(int32, uintptr, int32) int32, h int32) string {
	buf := make([]byte, 512)
	n := fn(h, uintptr(unsafe.Pointer(&buf[0])), int32(len(buf)))
	if n < 0 {
		return ""
	}
	return string(buf[:n])
}

func getStringIdx(fn func(int32, int32, uintptr, int32) int32, h, idx int32) string {
	buf := make([]byte, 512)
	n := fn(h, idx, uintptr(unsafe.Pointer(&buf[0])), int32(len(buf)))
	if n < 0 {
		return ""
	}
	return string(buf[:n])
}

func expectS(fn func(int32, uintptr, int32) int32, h int32, want, label string) {
	got := getString(fn, h)
	if got != want {
		fmt.Fprintf(os.Stderr, "FAIL: %s = %q, want %q\n", label, got, want)
		os.Exit(1)
	}
	fmt.Printf("PASS: %s = %q\n", label, got)
}

func expectI64(got, want int64, label string) {
	if got != want {
		fmt.Fprintf(os.Stderr, "FAIL: %s = %d, want %d\n", label, got, want)
		os.Exit(1)
	}
	fmt.Printf("PASS: %s = %d\n", label, got)
}

func expectContainsFile(fileFn func(int32, int32, uintptr, int32) int32, countFn func(int32) int32, h int32, want, label string) {
	count := countFn(h)
	for i := int32(0); i < count; i++ {
		got := getStringIdx(fileFn, h, i)
		if got == want {
			fmt.Printf("PASS: %s found at index %d\n", label, i)
			return
		}
	}
	fmt.Fprintf(os.Stderr, "FAIL: %s not found in %d extra files\n", label, count)
	os.Exit(1)
}

func expectContainsLdflag(flagFn func(int32, int32, uintptr, int32) int32, countFn func(int32) int32, h int32, want, label string) {
	count := countFn(h)
	for i := int32(0); i < count; i++ {
		got := getStringIdx(flagFn, h, i)
		if got == want {
			fmt.Printf("PASS: %s found at index %d\n", label, i)
			return
		}
	}
	fmt.Fprintf(os.Stderr, "FAIL: %s not found in %d ldflags\n", label, count)
	os.Exit(1)
}

func testOptLevel(fn func(uintptr, int32, uintptr, uintptr) int32, opt string, wantSpeed, wantSize int32) {
	data := []byte(opt)
	var speed, size int32
	rc := fn(uintptr(unsafe.Pointer(&data[0])), int32(len(data)),
		uintptr(unsafe.Pointer(&speed)), uintptr(unsafe.Pointer(&size)))
	if rc != 0 {
		fmt.Fprintf(os.Stderr, "FAIL: opt_level(%q) returned %d\n", opt, rc)
		os.Exit(1)
	}
	if speed != wantSpeed || size != wantSize {
		fmt.Fprintf(os.Stderr, "FAIL: opt_level(%q) = (%d,%d), want (%d,%d)\n", opt, speed, size, wantSpeed, wantSize)
		os.Exit(1)
	}
	fmt.Printf("PASS: opt_level(%q) = speed=%d, size=%d\n", opt, speed, size)
}

func testCanonicalArch(fn func(uintptr, int32, uintptr, int32) int32, triple, want string) {
	data := []byte(triple)
	buf := make([]byte, 128)
	n := fn(uintptr(unsafe.Pointer(&data[0])), int32(len(data)),
		uintptr(unsafe.Pointer(&buf[0])), int32(len(buf)))
	got := string(buf[:n])
	if got != want {
		fmt.Fprintf(os.Stderr, "FAIL: canonical_arch(%q) = %q, want %q\n", triple, got, want)
		os.Exit(1)
	}
	fmt.Printf("PASS: canonical_arch(%q) = %q\n", triple, got)
}

func openLib(path string) (uintptr, error) {
	switch runtime.GOOS {
	case "linux", "darwin":
		return purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_GLOBAL)
	default:
		return 0, fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}
