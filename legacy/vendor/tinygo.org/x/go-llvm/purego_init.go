//go:build purego

package llvm

import (
	"fmt"
	"os"
	"path/filepath"
	"unsafe"

	"github.com/ebitengine/purego"
)

func init() {
	llvmPath := os.Getenv("LLVM_LIB_PATH")
	if llvmPath == "" {
		llvmPath = findLLVM()
	}
	if llvmPath == "" {
		fmt.Fprintf(os.Stderr, "purego LLVM: cannot find libLLVM-22; set LLVM_LIB_PATH\n")
		os.Exit(1)
	}

	gluePath := os.Getenv("LLVM_GLUE_PATH")
	if gluePath == "" {
		gluePath = findGlue()
	}

	if err := Init(llvmPath, gluePath); err != nil {
		fmt.Fprintf(os.Stderr, "purego LLVM init: %v\n", err)
		os.Exit(1)
	}

	sym := trySym(libLLVM, "LLVMGetVersion")
	if sym != 0 {
		var major, minor, patch uint32
		purego.SyscallN(sym,
			uintptr(unsafe.Pointer(&major)),
			uintptr(unsafe.Pointer(&minor)),
			uintptr(unsafe.Pointer(&patch)))
		Version = fmt.Sprintf("%d.%d.%d", major, minor, patch)
	} else {
		Version = "19.0.0"
	}
}

func findLLVM() string {
	candidates := []string{
		"/usr/lib/libLLVM-22.so",
		"/usr/lib64/libLLVM-22.so",
		"/usr/lib/llvm-22/lib/libLLVM-22.so",
		"/usr/lib/llvm22/lib/libLLVM-22.so",
		"/usr/local/lib/libLLVM-22.so",
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func findGlue() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	dir := filepath.Dir(exe)
	candidates := []string{
		filepath.Join(dir, "libmoxie-llvm-glue.so"),
		filepath.Join(dir, "..", "lib", "libmoxie-llvm-glue.so"),
		filepath.Join(dir, "..", "llvm-glue", "libmoxie-llvm-glue.so"),
	}
	moxieRoot := os.Getenv("MOXIEROOT")
	if moxieRoot != "" {
		candidates = append(candidates, filepath.Join(moxieRoot, "llvm-glue", "libmoxie-llvm-glue.so"))
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}
