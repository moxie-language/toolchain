package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"unsafe"

	"github.com/ebitengine/purego"
)

const testMXH = `// mxh v1 abc123def456
type Foo struct {
    X uint32
    Y [20]byte
}
func (Foo) EncodeTo(w io.Writer) error
func (*Foo) DecodeFrom(r io.Reader) error
type Bar struct {
    Z int64
}
func (Bar) EncodeTo(w io.Writer) error
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: %s <path-to-parsemxh.so>\n", os.Args[0])
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

	var mxhParse func(uintptr, int32) int32
	purego.RegisterLibFunc(&mxhParse, lib, "moxie_mxh_parse")

	var mxhHash func(int32, uintptr, int32) int32
	purego.RegisterLibFunc(&mxhHash, lib, "moxie_mxh_hash")

	var mxhTypeCount func(int32) int32
	purego.RegisterLibFunc(&mxhTypeCount, lib, "moxie_mxh_type_count")

	var mxhTypeName func(int32, int32, uintptr, int32) int32
	purego.RegisterLibFunc(&mxhTypeName, lib, "moxie_mxh_type_name")

	var mxhTypeFieldCount func(int32, int32) int32
	purego.RegisterLibFunc(&mxhTypeFieldCount, lib, "moxie_mxh_type_field_count")

	var mxhTypeFieldName func(int32, int32, int32, uintptr, int32) int32
	purego.RegisterLibFunc(&mxhTypeFieldName, lib, "moxie_mxh_type_field_name")

	var mxhTypeFieldType func(int32, int32, int32, uintptr, int32) int32
	purego.RegisterLibFunc(&mxhTypeFieldType, lib, "moxie_mxh_type_field_type")

	var mxhTypeHasEncode func(int32, int32) int32
	purego.RegisterLibFunc(&mxhTypeHasEncode, lib, "moxie_mxh_type_has_encode")

	var mxhTypeHasDecode func(int32, int32) int32
	purego.RegisterLibFunc(&mxhTypeHasDecode, lib, "moxie_mxh_type_has_decode")

	var mxhFree func(int32)
	purego.RegisterLibFunc(&mxhFree, lib, "moxie_mxh_free")

	// Parse the test data.
	data := []byte(testMXH)
	handle := mxhParse(uintptr(unsafe.Pointer(&data[0])), int32(len(data)))
	if handle < 0 {
		fmt.Fprintf(os.Stderr, "FAIL: moxie_mxh_parse returned %d\n", handle)
		os.Exit(1)
	}
	fmt.Printf("PASS: moxie_mxh_parse returned handle %d\n", handle)

	// Verify hash.
	hash := getString(mxhHash, handle)
	expect(hash, "abc123def456", "hash")

	// Verify type count.
	tc := mxhTypeCount(handle)
	expectInt(tc, 2, "type_count")

	// Verify Foo.
	expectStr(mxhTypeName, handle, 0, "Foo", "type[0].name")
	expectInt(mxhTypeFieldCount(handle, 0), 2, "Foo.field_count")
	expectStrF(mxhTypeFieldName, handle, 0, 0, "X", "Foo.field[0].name")
	expectStrF(mxhTypeFieldType, handle, 0, 0, "uint32", "Foo.field[0].type")
	expectStrF(mxhTypeFieldName, handle, 0, 1, "Y", "Foo.field[1].name")
	expectStrF(mxhTypeFieldType, handle, 0, 1, "[20]byte", "Foo.field[1].type")
	expectInt(mxhTypeHasEncode(handle, 0), 1, "Foo.has_encode")
	expectInt(mxhTypeHasDecode(handle, 0), 1, "Foo.has_decode")

	// Verify Bar.
	expectStr(mxhTypeName, handle, 1, "Bar", "type[1].name")
	expectInt(mxhTypeFieldCount(handle, 1), 1, "Bar.field_count")
	expectStrF(mxhTypeFieldName, handle, 1, 0, "Z", "Bar.field[0].name")
	expectStrF(mxhTypeFieldType, handle, 1, 0, "int64", "Bar.field[0].type")
	expectInt(mxhTypeHasEncode(handle, 1), 1, "Bar.has_encode")
	expectInt(mxhTypeHasDecode(handle, 1), 0, "Bar.has_decode")

	// Free the handle.
	mxhFree(handle)

	// Verify freed handle returns -1.
	if mxhTypeCount(handle) != -1 {
		fmt.Fprintf(os.Stderr, "FAIL: freed handle still accessible\n")
		os.Exit(1)
	}
	fmt.Println("PASS: freed handle returns -1")

	fmt.Println("\nAll parsemxh roundtrip tests passed.")
}

func getString(fn func(int32, uintptr, int32) int32, handle int32) string {
	buf := make([]byte, 256)
	n := fn(handle, uintptr(unsafe.Pointer(&buf[0])), int32(len(buf)))
	if n < 0 {
		return ""
	}
	return string(buf[:n])
}

func expect(got, want, label string) {
	if got != want {
		fmt.Fprintf(os.Stderr, "FAIL: %s = %q, want %q\n", label, got, want)
		os.Exit(1)
	}
	fmt.Printf("PASS: %s = %q\n", label, got)
}

func expectInt(got, want int32, label string) {
	if got != want {
		fmt.Fprintf(os.Stderr, "FAIL: %s = %d, want %d\n", label, got, want)
		os.Exit(1)
	}
	fmt.Printf("PASS: %s = %d\n", label, got)
}

func expectStr(fn func(int32, int32, uintptr, int32) int32, handle, idx int32, want, label string) {
	buf := make([]byte, 256)
	n := fn(handle, idx, uintptr(unsafe.Pointer(&buf[0])), int32(len(buf)))
	got := ""
	if n >= 0 {
		got = string(buf[:n])
	}
	expect(got, want, label)
}

func expectStrF(fn func(int32, int32, int32, uintptr, int32) int32, handle, tidx, fidx int32, want, label string) {
	buf := make([]byte, 256)
	n := fn(handle, tidx, fidx, uintptr(unsafe.Pointer(&buf[0])), int32(len(buf)))
	got := ""
	if n >= 0 {
		got = string(buf[:n])
	}
	expect(got, want, label)
}

func openLib(path string) (uintptr, error) {
	switch runtime.GOOS {
	case "linux", "darwin":
		return purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_GLOBAL)
	default:
		return 0, fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}
