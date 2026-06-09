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
	patchRecording(dstDir)
	patchIdentical(dstDir)
	patchUnify(dstDir)
	patchTypeTerm(dstDir)
	patchSpawnBuiltin(dstDir)
	patchFreeBuiltin(dstDir)

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

	// Patch 7: Allow | on matching slice types (slice concatenation).
	// Also allow | when either operand is a string (string=[]byte unification).
	// After matchTypes + patch 4, an untyped string constant paired with []byte
	// becomes typed String (not []byte, since constants cannot be slice-typed),
	// so we must detect slice-ness on either side.
	src = mustReplace(f, src,
		"\tif !check.op(binaryOpPredicates, x, op) {\n\t\tx.mode = invalid\n\t\treturn\n\t}",
		"\t// Moxie: | on matching slice types is concatenation.\n\t_, mxSliceX := under(x.typ).(*Slice)\n\t_, mxSliceY := under(y.typ).(*Slice)\n\tmxSliceConcat := mxSliceX || mxSliceY || isString(x.typ) || isString(y.typ)\n\tif !(op == token.OR && mxSliceConcat) && !check.op(binaryOpPredicates, x, op) {\n\t\tx.mode = invalid\n\t\treturn\n\t}")

	// Patch 12: allow untyped string ↔ []byte conversion in matchTypes.mayConvert.
	// Without this, `"lit" | boundary` (where boundary is []byte) fails because
	// mayConvert short-circuits on allString mismatch, leaving the mixed-kind
	// operands for the binary-op predicate check which then rejects them.
	src = mustReplace(f, src,
		"\t\t// A string type can only convert to another string type.\n\t\tif allString(x.typ) != allString(y.typ) {\n\t\t\treturn false\n\t\t}",
		"\t\t// A string type can only convert to another string type.\n\t\tif allString(x.typ) != allString(y.typ) {\n\t\t\t// Moxie: string and []byte unify — allow conversion in either direction.\n\t\t\tif allString(x.typ) && isByteSlice(y.typ) {\n\t\t\t\treturn true\n\t\t\t}\n\t\t\tif isByteSlice(x.typ) && allString(y.typ) {\n\t\t\t\treturn true\n\t\t\t}\n\t\t\treturn false\n\t\t}")

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

func patchRecording(dir string) {
	f := filepath.Join(dir, "recording.go")
	src := mustRead(f)

	// Patch 11: Allow []byte as a valid constant type in type recording.
	// With string=[]byte, string constants may have []byte type.
	src = mustReplace(f, src,
		"assert(!isValid(typ) || allBasic(typ, IsConstType))",
		"assert(!isValid(typ) || allBasic(typ, IsConstType) || isByteSlice(typ))")

	mustWrite(f, src)
}

func patchIdentical(dir string) {
	f := filepath.Join(dir, "predicates.go")
	src := mustRead(f)

	// Patch 9: string ↔ []byte identity in comparer.identical.
	// Makes Identical(string, []byte) true, propagating through all composite
	// types: []string = [][]byte, map[string]V = map[[]byte]V, etc.
	src = mustReplace(f, src,
		"\tswitch x := x.(type) {\n\tcase *Basic:\n\t\t// Basic types are singletons except for the rune and byte\n\t\t// aliases, thus we cannot solely rely on the x == y check\n\t\t// above. See also comment in TypeName.IsAlias.\n\t\tif y, ok := y.(*Basic); ok {\n\t\t\treturn x.kind == y.kind\n\t\t}",
		"\t// Moxie: string and []byte are identical types.\n\tif (isString(x) && isByteSlice(y)) || (isByteSlice(x) && isString(y)) {\n\t\treturn true\n\t}\n\n\tswitch x := x.(type) {\n\tcase *Basic:\n\t\t// Basic types are singletons except for the rune and byte\n\t\t// aliases, thus we cannot solely rely on the x == y check\n\t\t// above. See also comment in TypeName.IsAlias.\n\t\tif y, ok := y.(*Basic); ok {\n\t\t\treturn x.kind == y.kind\n\t\t}")

	mustWrite(f, src)
}

func patchUnify(dir string) {
	f := filepath.Join(dir, "unify.go")
	src := mustRead(f)

	// Patch 10: string ↔ []byte unification in the type unifier.
	// Generic constraint satisfaction uses unify(), not Identical().
	src = mustReplace(f, src,
		"\t// nothing to do if x == y\n\tif x == y || Unalias(x) == Unalias(y) {\n\t\treturn true\n\t}",
		"\t// nothing to do if x == y\n\tif x == y || Unalias(x) == Unalias(y) {\n\t\treturn true\n\t}\n\n\t// Moxie: string and []byte unify (string=[]byte).\n\tif (isString(x) && isByteSlice(y)) || (isByteSlice(x) && isString(y)) {\n\t\treturn true\n\t}")

	mustWrite(f, src)
}

func patchTypeTerm(dir string) {
	f := filepath.Join(dir, "typeterm.go")
	src := mustRead(f)

	// Patch 8: Generic constraint satisfaction — []byte satisfies ~string.
	// term.includes checks Identical(x.typ, under(t)). With string=[]byte,
	// []byte must satisfy ~string in constraint unions like cmp.Ordered.
	src = mustReplace(f, src,
		"\treturn Identical(x.typ, u)\n}",
		"\tif Identical(x.typ, u) {\n\t\treturn true\n\t}\n\t// Moxie: []byte satisfies ~string (string=[]byte unification).\n\tif isString(x.typ) && isByteSlice(t) {\n\t\treturn true\n\t}\n\tif isByteSlice(x.typ) && isString(t) {\n\t\treturn true\n\t}\n\treturn false\n}")

	mustWrite(f, src)
}

func patchSpawnBuiltin(dir string) {
	// Patch universe.go: add _Spawn builtin ID and predeclaredFuncs entry.
	f := filepath.Join(dir, "universe.go")
	src := mustRead(f)

	// Add _Spawn after _Recover in the builtinId enum.
	src = mustReplace(f, src,
		"\t_Recover\n\n\t// package unsafe",
		"\t_Recover\n\t_Spawn\n\n\t// package unsafe")

	// Add spawn entry in predeclaredFuncs after _Recover.
	// nargs=1 (minimum: the function), variadic=true, statement-kind so the
	// call can be used as an expression statement (its primary use) while
	// still allowing `ch := spawn(...)` to consume the lifecycle handle.
	src = mustReplace(f, src,
		"\t_Recover: {\"recover\", 0, false, statement},",
		"\t_Recover: {\"recover\", 0, false, statement},\n\t_Spawn:   {\"spawn\", 1, true, statement},")

	mustWrite(f, src)

	// Patch builtins.go: add type-checking handler for spawn.
	f = filepath.Join(dir, "builtins.go")
	src = mustRead(f)

	// Insert spawn handler after the _Recover case, before _Add.
	src = mustReplace(f, src,
		"\tcase _Recover:\n\t\t// recover() interface{}\n\t\tx.mode = value\n\t\tx.typ = &emptyInterface\n\t\tif check.recordTypes() {\n\t\t\tcheck.recordBuiltinType(call.Fun, makeSig(x.typ))\n\t\t}\n\n\tcase _Add:",
		`	case _Recover:
		// recover() interface{}
		x.mode = value
		x.typ = &emptyInterface
		if check.recordTypes() {
			check.recordBuiltinType(call.Fun, makeSig(x.typ))
		}

	case _Spawn:
		// spawn(fn, args...) chan struct{}
		// spawn("transport", fn, args...) chan struct{}
		// First argument must be a function or a transport string constant.
		// Remaining arguments are validated by the compiler at SSA level
		// (must implement moxie.Codec, channels must have Codec element types).
		if nargs < 1 {
			return
		}
		if _, ok := under(x.typ).(*Signature); !ok {
			if !isString(x.typ) {
				check.errorf(x, InvalidCall, invalidOp+"first argument to spawn must be a function or transport string, got %s", x)
				return
			}
			// Transport string — second arg must be a function.
			if nargs < 2 {
				check.errorf(x, WrongArgCount, invalidOp+"spawn with transport string requires a function argument")
				return
			}
			if _, ok := under(args[1].typ).(*Signature); !ok {
				check.errorf(args[1], InvalidCall, invalidOp+"second argument to spawn must be a function when first is a transport string, got %s", args[1])
				return
			}
		}
		x.mode = value
		x.typ = NewChan(SendRecv, NewStruct(nil, nil))
		// Synthesize a signature for the spawn ident so the SSA builder
		// can construct a *Builtin with a *types.Signature when it visits
		// the call. Without this, fn.instanceType(e).(*types.Signature)
		// at builder.go:814 panics: the universe entry has no signature.
		if check.recordTypes() {
			params := make([]Type, nargs)
			for i, a := range args {
				params[i] = a.typ
			}
			check.recordBuiltinType(call.Fun, makeSig(x.typ, params...))
		}

	case _Add:`)

	mustWrite(f, src)
}

func patchFreeBuiltin(dir string) {
	f := filepath.Join(dir, "universe.go")
	src := mustRead(f)

	src = mustReplace(f, src,
		"\t_Spawn\n\n\t// package unsafe",
		"\t_Spawn\n\t_Free\n\n\t// package unsafe")

	src = mustReplace(f, src,
		"\t_Spawn:   {\"spawn\", 1, true, statement},",
		"\t_Spawn:   {\"spawn\", 1, true, statement},\n\t_Free:    {\"free\", 1, false, statement},")

	mustWrite(f, src)

	f = filepath.Join(dir, "builtins.go")
	src = mustRead(f)

	src = mustReplace(f, src,
		"\tcase _Add:",
		"\tcase _Free:\n\t\t// free(x) - deterministic deallocation\n\t\tx.mode = novalue\n\t\tif check.recordTypes() {\n\t\t\tcheck.recordBuiltinType(call.Fun, makeSig(nil, x.typ))\n\t\t}\n\n\tcase _Add:")

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
