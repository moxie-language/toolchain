// build/patch-gotypes.go — Patches $GOROOT/src/go/types for Moxie string=[]byte.
// Run: go run build/patch-gotypes.go <target-dir>
// Copies go/types to <target-dir>/src/go/types/ and applies 5 Moxie patches.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: go run build/patch-gotypes.go <goroot-target>")
		os.Exit(1)
	}
	target := os.Args[1]
	goroot := os.Getenv("GOROOT")
	if goroot == "" {
		out, err := exec.Command("go", "env", "GOROOT").Output()
		if err != nil {
			fatal("cannot determine GOROOT: %v", err)
		}
		goroot = strings.TrimSpace(string(out))
	}

	srcDir := filepath.Join(goroot, "src", "go", "types")
	dstDir := filepath.Join(target, "src", "go", "types")

	// Copy go/types source.
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		fatal("mkdir: %v", err)
	}
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		fatal("readdir %s: %v", srcDir, err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(srcDir, e.Name()))
		if err != nil {
			fatal("read %s: %v", e.Name(), err)
		}
		if err := os.WriteFile(filepath.Join(dstDir, e.Name()), data, 0o644); err != nil {
			fatal("write %s: %v", e.Name(), err)
		}
	}
	// Copy testdata dir if it exists (needed for go/types to compile).
	tdSrc := filepath.Join(srcDir, "testdata")
	if info, err := os.Stat(tdSrc); err == nil && info.IsDir() {
		tdDst := filepath.Join(dstDir, "testdata")
		cpCmd := exec.Command("cp", "-a", tdSrc, tdDst)
		if out, err := cpCmd.CombinedOutput(); err != nil {
			fatal("copy testdata: %s: %v", out, err)
		}
	}

	// Apply patches.
	patchPredicates(dstDir)
	patchOperand(dstDir)
	patchExpr(dstDir)
	patchSizes(dstDir)
	patchDecl(dstDir)

	fmt.Fprintf(os.Stderr, "patched go/types in %s\n", dstDir)
}

func patchPredicates(dir string) {
	f := filepath.Join(dir, "predicates.go")
	src := mustRead(f)

	// Patch 1: Add isByteSlice helper after isString.
	src = mustReplace(f, src,
		"func isString(t Type) bool         { return isBasic(t, IsString) }\n",
		`func isString(t Type) bool         { return isBasic(t, IsString) }
func isByteSlice(t Type) bool {
	if s, ok := under(t).(*Slice); ok {
		if b, ok := s.elem.(*Basic); ok && b.kind == Byte {
			return true
		}
	}
	return false
}
`)

	// Patch 2: Add slice comparability — any slice of comparable elements is comparable.
	src = mustReplace(f, src,
		"\tdefault:\n\t\treturn typeErrorf(\"\")\n\t}\n\n\treturn nil\n}",
		"\tcase *Slice:\n\t\t// Moxie: slices of comparable types are comparable.\n\t\t// Length check + element-wise comparison at runtime.\n\t\treturn comparableType(t.elem, dynamic, seen)\n\n\tdefault:\n\t\treturn typeErrorf(\"\")\n\t}\n\n\treturn nil\n}")

	mustWrite(f, src)
}

func patchOperand(dir string) {
	f := filepath.Join(dir, "operand.go")
	src := mustRead(f)

	// Patch 3: string↔[]byte mutual assignability.
	src = mustReplace(f, src,
		"\t// T is an interface type, but not a type parameter, and V implements T.\n\t// Also handle the case where T is a pointer to an interface so that we get\n\t// the Checker.implements error cause.",
		"\t// Moxie: string and []byte are mutually assignable.\n\tif (isString(Vu) && isByteSlice(Tu)) || (isByteSlice(Vu) && isString(Tu)) {\n\t\treturn true, 0\n\t}\n\n\t// T is an interface type, but not a type parameter, and V implements T.\n\t// Also handle the case where T is a pointer to an interface so that we get\n\t// the Checker.implements error cause.")

	mustWrite(f, src)
}

func patchExpr(dir string) {
	f := filepath.Join(dir, "expr.go")
	src := mustRead(f)

	// Patch 4: Untyped string → []byte implicit conversion.
	// For constants, return Typ[String] (not []byte) because slices aren't
	// valid constant types. The assignableTo patch handles string→[]byte.
	src = mustReplace(f, src,
		"\tswitch u := under(target).(type) {\n\tcase *Basic:\n\t\tif x.mode == constant_ {",
		"\t// Moxie: untyped string → []byte implicit conversion (string=[]byte unification).\n\t// For constants, return Typ[String] because slices aren't valid constant types.\n\tif x.typ.(*Basic).kind == UntypedString && isByteSlice(target) {\n\t\tif x.mode == constant_ {\n\t\t\treturn Typ[String], x.val, 0\n\t\t}\n\t\treturn target, nil, 0\n\t}\n\n\tswitch u := under(target).(type) {\n\tcase *Basic:\n\t\tif x.mode == constant_ {")

	mustWrite(f, src)
}

func patchSizes(dir string) {
	f := filepath.Join(dir, "sizes.go")
	src := mustRead(f)

	// Patch 5: String size = 3 words (ptr, len, cap — matching []byte layout).
	src = mustReplace(f, src,
		"\t\tif k == String {\n\t\t\treturn s.WordSize * 2\n\t\t}",
		"\t\tif k == String {\n\t\t\t// Moxie: string=[]byte — 3-word struct (ptr, len, cap).\n\t\t\treturn s.WordSize * 3\n\t\t}")

	mustWrite(f, src)
}

func patchDecl(dir string) {
	f := filepath.Join(dir, "decl.go")
	src := mustRead(f)

	// Patch 6: Allow []byte as a valid constant type (string=[]byte unification).
	// With string=[]byte, named types like `type X []byte` should accept string constants.
	src = mustReplace(f, src,
		"\t\tif !isConstType(t) {",
		"\t\tif !isConstType(t) && !isByteSlice(t) {")

	mustWrite(f, src)
}

func mustRead(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		fatal("read %s: %v", path, err)
	}
	return string(data)
}

func mustWrite(path string, content string) {
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		fatal("write %s: %v", path, err)
	}
}

func mustReplace(file, src, old, new string) string {
	if !strings.Contains(src, old) {
		fatal("patch target not found in %s:\n---\n%s\n---", file, old)
	}
	count := strings.Count(src, old)
	if count != 1 {
		fatal("patch target appears %d times in %s (expected 1)", count, file)
	}
	return strings.Replace(src, old, new, 1)
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "patch-gotypes: "+format+"\n", args...)
	os.Exit(1)
}
