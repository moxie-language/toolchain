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
	patchMoxieSliceBuiltins(dstDir)
	patchTypeOfBuiltin(dstDir)
	patchRemovePredeclaredTypes(dstDir)
	patchErrorString(dstDir)
	patchPanicRecoverString(dstDir)
	patchExportAccess(dstDir)
	patchScopeHashPrefix(dstDir)

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

func patchMoxieSliceBuiltins(dir string) {
	// Patch universe.go: add _Push, _Pop, _Resize builtin IDs and
	// predeclaredFuncs entries after _Spawn.
	f := filepath.Join(dir, "universe.go")
	src := mustRead(f)

	src = mustReplace(f, src,
		"\t_Recover\n\t_Spawn\n\n\t// package unsafe",
		"\t_Recover\n\t_Spawn\n\t_Push\n\t_Pop\n\t_Resize\n\n\t// package unsafe")

	src = mustReplace(f, src,
		"\t_Spawn:   {\"spawn\", 1, true, statement},",
		"\t_Spawn:   {\"spawn\", 1, true, statement},\n\t_Push:    {\"push\", 1, true, statement},\n\t_Pop:     {\"pop\", 1, false, statement},\n\t_Resize:  {\"resize\", 2, false, statement},")

	// make/new are not predeclared in Moxie: the loader lowers literal
	// []T{:n} / chan T{} syntax to the internal __slicealloc/__ptralloc
	// names, which retain the make/new builtin semantics. User-written
	// make(...)/new(...) therefore resolve as undefined.
	src = mustReplace(f, src,
		"\t_Make:    {\"make\", 1, true, expression},",
		"\t_Make:    {\"__slicealloc\", 1, true, expression},")
	src = mustReplace(f, src,
		"\t_New:     {\"new\", 1, false, expression},",
		"\t_New:     {\"__ptralloc\", 1, false, expression},")

	// append is not predeclared for users: the compiler's own packages use
	// the internal __moxie_append name, which retains append semantics.
	src = mustReplace(f, src,
		"\t_Append:  {\"append\", 1, true, expression},\n",
		"\t_Append:  {\"__moxie_append\", 1, true, expression},\n")

	// complex numbers are removed from Moxie: the complex/real/imag builtins
	// resolve as undefined.
	src = mustReplace(f, src,
		"\t_Complex: {\"complex\", 2, false, expression},\n",
		"")
	src = mustReplace(f, src,
		"\t_Imag:    {\"imag\", 1, false, expression},\n",
		"")
	src = mustReplace(f, src,
		"\t_Real:    {\"real\", 1, false, expression},\n",
		"")

	mustWrite(f, src)

	// Patch builtins.go: allow ... on push (like append) and add
	// type-checking handlers for push/pop/resize.
	f = filepath.Join(dir, "builtins.go")
	src = mustRead(f)

	src = mustReplace(f, src,
		"\t// append is the only built-in that permits the use of ... for the last argument\n\tbin := predeclaredFuncs[id]\n\tif hasDots(call) && id != _Append {",
		"\t// append and push are the only built-ins that permit the use of ... for the last argument\n\tbin := predeclaredFuncs[id]\n\tif hasDots(call) && id != _Append && id != _Push {")

	src = mustReplace(f, src,
		"\tcase _Recover:\n\t\t// recover() interface{}\n\t\tx.mode = value\n\t\tx.typ = &emptyInterface\n\t\tif check.recordTypes() {\n\t\t\tcheck.recordBuiltinType(call.Fun, makeSig(x.typ))\n\t\t}\n\n\tcase _Spawn:",
		`	case _Recover:
		// recover() interface{}
		x.mode = value
		x.typ = &emptyInterface
		if check.recordTypes() {
			check.recordBuiltinType(call.Fun, makeSig(x.typ))
		}

	case _Push:
		// push(s S, elems ...E) S, where E is the element type of S.
		// Mirrors _Append's element conversion: untyped constants are
		// converted to E, and spread push(s, e...) with e assignable to
		// S or []E is accepted. Type parameters (S ~[]E) are resolved
		// via sliceElem, same as append.
		if nargs < 1 {
			return
		}
		E, err := sliceElem(x)
		var sig *Signature
		if err == nil {
			sig = makeSig(x.typ, x.typ, NewSlice(E))
			sig.variadic = true
			check.arguments(call, sig, nil, nil, args, nil)
			x.mode = value
			x.typ = args[0].typ
		} else if isString(args[0].typ) {
			// string=[]byte: allow []byte via string
			sig = makeSig(args[0].typ, args[0].typ, NewSlice(universeByte))
			sig.variadic = true
			check.arguments(call, sig, nil, nil, args, nil)
			x.mode = value
			x.typ = args[0].typ
		} else {
			check.errorf(x, InvalidCall, "invalid push: %s", err.format(check))
			return
		}
		if check.recordTypes() {
			check.recordBuiltinType(call.Fun, sig)
		}

	case _Pop:
		// pop(s) returns the last element of s.
		if nargs != 1 {
			check.errorf(x, WrongArgCount, invalidOp+"pop requires exactly one argument")
			return
		}
		switch t := under(args[0].typ).(type) {
		case *Slice:
			x.mode = value
			x.typ = t.elem
		default:
			if isString(args[0].typ) {
				x.mode = value
				x.typ = Typ[Byte]
			} else {
				check.errorf(args[0], InvalidCall, invalidOp+"first argument to pop must be a slice, got %s", args[0])
			}
		}
		if check.recordTypes() {
			check.recordBuiltinType(call.Fun, makeSig(x.typ, args[0].typ))
		}

	case _Resize:
		// resize(s, n) returns s with length n. The n argument is
		// checked against int32 so untyped constants are converted.
		if nargs < 2 {
			return
		}
		switch under(args[0].typ).(type) {
		case *Slice:
			x.mode = value
			x.typ = args[0].typ
		default:
			if isString(args[0].typ) {
				x.mode = value
				x.typ = args[0].typ
			} else {
				check.errorf(args[0], InvalidCall, invalidOp+"first argument to resize must be a slice, got %s", args[0])
			}
		}
		sig := makeSig(x.typ, x.typ, Typ[Int32])
		check.arguments(call, sig, nil, nil, args, nil)
		if check.recordTypes() {
			check.recordBuiltinType(call.Fun, sig)
		}

	case _Spawn:`)

	mustWrite(f, src)
}

func patchTypeOfBuiltin(dir string) {
	// Patch universe.go: add _TypeOf builtin ID and predeclaredFuncs entry.
	f := filepath.Join(dir, "universe.go")
	src := mustRead(f)

	src = mustReplace(f, src,
		"\t_StringData\n\n\t// testing support",
		"\t_StringData\n\t_TypeOf\n\n\t// testing support")

	src = mustReplace(f, src,
		"\t_StringData: {\"StringData\", 1, false, expression},",
		"\t_StringData: {\"StringData\", 1, false, expression},\n\t_TypeOf:     {\"TypeOf\", 1, false, expression},")

	mustWrite(f, src)

	// Patch builtins.go: type-check handler for unsafe.TypeOf.
	f = filepath.Join(dir, "builtins.go")
	src = mustRead(f)

	src = mustReplace(f, src,
		`		x.typ = Typ[Uintptr]

	case _Slice:`,
		`		x.typ = Typ[Uintptr]

	case _TypeOf:
		// unsafe.TypeOf(x T) unsafe.Pointer
		check.assignment(x, nil, "argument to unsafe.TypeOf")
		if x.mode == invalid {
			return
		}
		x.mode = value
		if check.recordTypes() {
			check.recordBuiltinType(call.Fun, makeSig(Typ[UnsafePointer], x.typ))
		}
		x.typ = Typ[UnsafePointer]

	case _Slice:`)

	mustWrite(f, src)
}

func patchRemovePredeclaredTypes(dir string) {
	// int, uint, complex64, complex128 are not predeclared in Moxie: they
	// resolve as undefined identifiers (a user may define their own). uintptr
	// is kept - it is the pointer-sized integer for unsafe.Pointer arithmetic.
	f := filepath.Join(dir, "universe.go")
	src := mustRead(f)
	src = mustReplace(f, src,
		"func defPredeclaredTypes() {\n\tfor _, t := range Typ {\n\t\tdef(NewTypeName(nopos, nil, t.name, t))\n\t}",
		"func defPredeclaredTypes() {\n\tfor _, t := range Typ {\n\t\tswitch t.name {\n\t\tcase \"int\", \"uint\", \"complex64\", \"complex128\":\n\t\t\tcontinue // Moxie: not predeclared - undefined\n\t\t}\n\t\tdef(NewTypeName(nopos, nil, t.name, t))\n\t}")

	// Removed builtins (make/new renamed, append/complex/real/imag dropped)
	// leave zero-value slots in predeclaredFuncs; skip empty names.
	src = mustReplace(f, src,
		"func defPredeclaredFuncs() {\n\tfor i := range predeclaredFuncs {\n\t\tid := builtinId(i)\n\t\tif id == _Assert || id == _Trace {\n\t\t\tcontinue // only define these in testing environment\n\t\t}\n\t\tdef(newBuiltin(id))\n\t}\n}",
		"func defPredeclaredFuncs() {\n\tfor i := range predeclaredFuncs {\n\t\tid := builtinId(i)\n\t\tif id == _Assert || id == _Trace {\n\t\t\tcontinue // only define these in testing environment\n\t\t}\n\t\tif predeclaredFuncs[i].name == \"\" {\n\t\t\tcontinue // Moxie: removed builtin\n\t\t}\n\t\tdef(newBuiltin(id))\n\t}\n}")

	// any (empty interface) is not predeclared in Moxie. The alias objects
	// stay defined (go/types internals reference them for formatting) but are
	// not inserted into the Universe scope, so "any" resolves as undefined.
	src = mustReplace(f, src,
		"\t\tuniverseAnyAlias.setColor(black)\n\t\t_ = NewAlias(universeAnyAlias, universeAnyNoAlias.Type().Underlying()) // Link TypeName and Alias\n\t\tdef(universeAnyAlias)",
		"\t\tuniverseAnyAlias.setColor(black)\n\t\t_ = NewAlias(universeAnyAlias, universeAnyNoAlias.Type().Underlying()) // Link TypeName and Alias\n\t\t// Moxie: any is not predeclared - undefined")
	mustWrite(f, src)
}

func patchErrorString(dir string) {
	f := filepath.Join(dir, "universe.go")
	src := mustRead(f)

	// Add String() method to the error interface alongside Error().
	// Moxie stdlib calls err.String() everywhere; error must satisfy stringer.
	src = mustReplace(f, src,
		"\t\t// interface{ Error() string }\n\t\tityp := &Interface{methods: []*Func{err}, complete: true}",
		"\t\t// Moxie: error also has String() so it satisfies stringer.\n\t\tstrFn := NewFunc(nopos, nil, \"String\", sig)\n\n\t\t// interface{ Error() string; String() string }\n\t\tityp := &Interface{methods: []*Func{err, strFn}, complete: true}")

	mustWrite(f, src)
}

func patchPanicRecoverString(dir string) {
	// Moxie: panic takes a string, recover returns a string. The empty
	// interface is elided, so these builtins no longer box/unbox to
	// interface{}. Callers pass a string (or an error's .Error()/.String()).
	f := filepath.Join(dir, "builtins.go")
	src := mustRead(f)

	src = mustReplace(f, src,
		"\t\tcheck.assignment(x, &emptyInterface, \"argument to panic\")",
		"\t\tcheck.assignment(x, Typ[String], \"argument to panic\")")

	src = mustReplace(f, src,
		"\tcase _Recover:\n\t\t// recover() interface{}\n\t\tx.mode = value\n\t\tx.typ = &emptyInterface",
		"\tcase _Recover:\n\t\t// recover() string\n\t\tx.mode = value\n\t\tx.typ = Typ[String]")

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

func patchScopeHashPrefix(dir string) {
	f := filepath.Join(dir, "scope.go")
	src := mustRead(f)

	// Add stripHash helper before Lookup.
	src = mustReplace(f, src,
		"func (s *Scope) Lookup(name string) Object {\n\tobj := resolve(name, s.elems[name])",
		"// stripHash removes a leading # from a Moxie export-annotated name.\n// #foo and foo are the same identifier; # is an annotation, not part of the name.\nfunc stripHash(name string) string {\n\tif len(name) > 0 && name[0] == '#' {\n\t\treturn name[1:]\n\t}\n\treturn name\n}\n\nfunc (s *Scope) Lookup(name string) Object {\n\tname = stripHash(name)\n\tobj := resolve(name, s.elems[name])")

	// Patch insert to strip # from key.
	src = mustReplace(f, src,
		"func (s *Scope) insert(name string, obj Object) {\n\tif s.elems == nil {\n\t\ts.elems = make(map[string]Object)\n\t}\n\ts.elems[name] = obj\n}",
		"func (s *Scope) insert(name string, obj Object) {\n\tname = stripHash(name)\n\tif s.elems == nil {\n\t\ts.elems = make(map[string]Object)\n\t}\n\ts.elems[name] = obj\n}")

	// Patch Insert to strip # from name for lookup.
	src = mustReplace(f, src,
		"func (s *Scope) Insert(obj Object) Object {\n\tname := obj.Name()\n\tif alt := s.Lookup(name); alt != nil {",
		"func (s *Scope) Insert(obj Object) Object {\n\tname := stripHash(obj.Name())\n\tif alt := s.Lookup(name); alt != nil {")

	mustWrite(f, src)
}

func patchExportAccess(dir string) {
	f := filepath.Join(dir, "call.go")
	src := mustRead(f)

	// Moxie: all identifiers are accessible cross-package.
	// Capital/# prefix marks stable API, not access control.
	// Remove the unexported name error so pkg.lowercaseName compiles.
	src = mustReplace(f, src,
		"\t\t\t\tif !exp.Exported() {\n\t\t\t\t\tcheck.errorf(e.Sel, UnexportedName, \"name %s not exported by package %s\", sel, pkg.name)\n\t\t\t\t\t// ok to continue\n\t\t\t\t}",
		"\t\t\t\t// Moxie: all identifiers accessible cross-package.\n\t\t\t\t// Capital/# prefix marks stable API contract, not access control.\n\t\t\t\t_ = exp.Exported()")

	mustWrite(f, src)
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "patch-gotypes: "+format+"\n", args...)
	os.Exit(1)
}
