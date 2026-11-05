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
	patchBuiltinMethods(dstDir)
	patchPtrRecvMethodSet(dstDir)
	patchArrayCapNegative(dstDir)
	patchChanFrameBudget(dstDir)
	patchSigintBuiltin(dstDir)

	fmt.Fprintf(os.Stderr, "patched go/types in %s\n", dstDir)
}

// patchPtrRecvMethodSet implements Moxie's method-set rule. Moxie bans value
// receivers, so a pointer-receiver method belongs to the method set of the
// value type T as well as *T: a value boxed into an interface is materialized
// into a fresh copy and the method runs on a pointer to that copy. Go's rule
// keeps a pointer-receiver method out of T's method set, which rejects
//
//	var e error = C(1)
//
// for a named type whose Error has a pointer receiver - and rejects the
// Errno-value-as-error returns that the stdlib produces once Errno's Error is
// a pointer receiver, which breaks the bootstrap. pkg/types (stage4) keeps the
// method in T's set deliberately (methodSet/collectMethods); this is the
// legacy mirror of that rule.
func patchPtrRecvMethodSet(dir string) {
	// methodset.go: include pointer-receiver methods in the value method set.
	// NewMethodSet is what the legacy compiler consults for a type's method
	// set (getTypeCode/getTypeMethodSet), so a value box needs the arm here.
	f := filepath.Join(dir, "methodset.go")
	src := mustRead(f)
	src = mustReplace(f, src,
		"\t\tif _, found := s[key]; !found && (indirect || !f.hasPtrRecv()) {",
		"\t\t// Moxie: all receivers are pointers, so a pointer-receiver method\n\t\t// is in the method set of the value type T as well as *T.\n\t\tif _, found := s[key]; !found {")
	mustWrite(f, src)

	// lookup.go: drop Go's addressability requirement for a pointer-receiver
	// method. In Moxie all receivers are pointers and a value expression is
	// auto-addressed into a copy, so a pointer-receiver method is callable on
	// a non-addressable value (Mk().Get()) exactly as it is in the value
	// type's method set. The same rule makes interface satisfaction
	// (missingMethod, which goes through this lookup) find Error on a value
	// whose Error has a pointer receiver.
	f = filepath.Join(dir, "lookup.go")
	src = mustRead(f)
	src = mustReplace(f, src,
		"\t\t\tif f, _ := obj.(*Func); f != nil {\n\t\t\t\t// determine if method has a pointer receiver\n\t\t\t\tif f.hasPtrRecv() && !indirect && !addressable {\n\t\t\t\t\treturn nil, nil, true // pointer/addressable receiver required\n\t\t\t\t}\n\t\t\t}",
		"\t\t\t// Moxie: all receivers are pointers and a value is auto-addressed\n\t\t\t// into a copy, so a pointer-receiver method is callable on a value\n\t\t\t// expression that is not addressable, and belongs to the value\n\t\t\t// type's method set. Go's addressability requirement is dropped.")
	mustWrite(f, src)
}

// patchBuiltinMethods lets a builtin type carry methods. src/runtime/stringer.mx
// declares String() on every builtin, and fmt dispatches through Stringer; Go
// forbids methods on non-local types, so without this the file cannot be
// compiled and the loader papers over it by deleting "missing method" errors.
// The method set is stored on the Basic itself (the Typ singletons are shared,
// so the methods are universal) and receiver base name resolution is taught to
// accept a universe type name.
func patchBuiltinMethods(dir string) {
	// basic.go: a side table of methods on builtin types. A field on Basic
	// would break the unkeyed literals that initialise Typ.
	f := filepath.Join(dir, "basic.go")
	src := mustRead(f)
	src = mustReplace(f, src,
		"// Kind returns the kind of basic type b.\nfunc (b *Basic) Kind() BasicKind { return b.kind }",
		"// Kind returns the kind of basic type b.\nfunc (b *Basic) Kind() BasicKind { return b.kind }\n\n// Moxie: methods declared on the builtin type (src/runtime/stringer.mx).\n// A map keyed by the Basic keeps the struct unchanged: Typ is initialised\n// with unkeyed literals. The Typ entries are universe singletons, so a\n// method recorded here is in the method set everywhere.\nvar basicMethods = map[*Basic][]*Func{}\n\n// defBuiltinStringMethods declares the builtin String() methods in the\n// universe, exactly as Moxie's own universe does. A package may be\n// type-checked before the runtime that declares them, and fmt.Stringer\n// satisfaction is decided at check time, so the method must already be\n// visible. The runtime's real declaration replaces the placeholder.\nfunc defBuiltinStringMethods() {\n\tfor _, b := range []*Basic{\n\t\tTyp[Bool],\n\t\tTyp[Int8], Typ[Int16], Typ[Int32], Typ[Int64],\n\t\tTyp[Uint8], Typ[Uint16], Typ[Uint32], Typ[Uint64],\n\t\tTyp[Float32], Typ[Float64],\n\t\tTyp[String],\n\t\tTyp[Uintptr],\n\t} {\n\t\t// The receiver is part of the signature even for a placeholder: SSA's\n\t\t// MethodValue dereferences it, and a receiver-less method crashed the\n\t\t// compiler on every wasm build (named types over builtins -\n\t\t// interrupt.State was the first). The runtime's real declaration\n\t\t// replaces it when that package is checked.\n\t\tsig := NewSignatureType(NewVar(nopos, nil, \"b\", NewPointer(b)), nil, nil, nil, NewTuple(NewVar(nopos, nil, \"\", Typ[String])), false)\n\t\tbasicMethods[b] = []*Func{NewFunc(nopos, nil, \"String\", sig)}\n\t}\n}\n\n// basicAddMethod records a method on the builtin type. A re-check of the\n// same package replaces its own object (the SSA package is built from the\n// newest pass); a declaration from another package is a redeclaration and\n// the existing method is returned so the caller can report it.\nfunc basicCanon(t *Basic) *Basic {\n\t// byte and rune are separate Basic values in go/types but the same type\n\t// as uint8 and int32, so their methods live on the canonical entry.\n\tswitch t.kind {\n\tcase Byte:\n\t\treturn Typ[Uint8]\n\tcase Rune:\n\t\treturn Typ[Int32]\n\t}\n\treturn t\n}\n\nfunc basicAddMethod(t *Basic, m *Func) *Func {\n\tt = basicCanon(t)\n\tfor i, e := range basicMethods[t] {\n\t\tif e.sameId(m.Pkg(), m.Name(), false) {\n\t\t\tif e.Pkg() == nil || (m.Pkg() != nil && e.Pkg().Path() == m.Pkg().Path()) {\n\t\t\t\tbasicMethods[t][i] = m\n\t\t\t\treturn nil\n\t\t\t}\n\t\t\treturn e\n\t\t}\n\t}\n\tbasicMethods[t] = append(basicMethods[t], m)\n\treturn nil\n}\nfunc basicNumMethods(t *Basic) int    { return len(basicMethods[basicCanon(t)]) }\nfunc basicMethod(t *Basic, i int) *Func { return basicMethods[basicCanon(t)][i] }\n\n// basicLookupMethod returns the index and method with the given package and name.\nfunc basicLookupMethod(t *Basic, pkg *Package, name string, foldCase bool) (int, *Func) {\n\tt = basicCanon(t)\n\tfor i, m := range basicMethods[t] {\n\t\tif m.sameId(pkg, name, foldCase) {\n\t\t\treturn i, m\n\t\t}\n\t}\n\treturn 0, nil\n}")
	mustWrite(f, src)

	// call.go: a selector on an untyped constant works on its fixed-width
	// default type. Moxie's defaults are int32/float64/string because int and
	// uint are not in the universe; Default() would hand back Typ[Int], which
	// has no methods and no LLVM type. Rebinding x.typ (not only the lookup
	// argument) is what makes SSA emit the call at that width - with a local
	// conversion the lookup found String() but the builder then panicked with
	// "unknown basic type: untyped int".
	f = filepath.Join(dir, "call.go")
	src = mustRead(f)
	src = mustReplace(f, src,
		"\tobj, index, indirect = lookupFieldOrMethod(x.typ, x.mode == variable, check.pkg, sel, false)",
		"\t// Moxie: an untyped constant's selector works on its fixed-width\n\t// default type; int and uint are not in the universe.\n\tif b, ok := x.typ.(*Basic); ok {\n\t\tswitch b.kind {\n\t\tcase UntypedBool:\n\t\t\tx.typ = Typ[Bool]\n\t\tcase UntypedInt:\n\t\t\tx.typ = Typ[Int32]\n\t\tcase UntypedRune:\n\t\t\tx.typ = Typ[Int32]\n\t\tcase UntypedFloat:\n\t\t\tx.typ = Typ[Float64]\n\t\tcase UntypedString:\n\t\t\tx.typ = Typ[String]\n\t\t}\n\t}\n\tobj, index, indirect = lookupFieldOrMethod(x.typ, x.mode == variable, check.pkg, sel, false)")
	mustWrite(f, src)

	// resolver.go: accept a universe type name as a receiver base, and attach
	// the method to the type when the base is a builtin.
	f = filepath.Join(dir, "resolver.go")
	src = mustRead(f)
	src = mustReplace(f, src,
		"\tfor name != nil {\n\t\t// name must denote an object found in the current package scope\n\t\t// (note that dot-imported objects are not in the package scope!)\n\t\tobj := check.pkg.scope.Lookup(name.Name)\n\t\tif obj == nil {\n\t\t\tbreak\n\t\t}",
		"\tfor name != nil {\n\t\t// name must denote an object found in the current package scope\n\t\t// (note that dot-imported objects are not in the package scope!)\n\t\tobj := check.pkg.scope.Lookup(name.Name)\n\t\tif obj == nil {\n\t\t\t// Moxie: a builtin type may be the receiver base type\n\t\t\t// (func (n int32) String()). Its methods are registered on the\n\t\t\t// type object itself, not in check.methods.\n\t\t\tif uobj := Universe.Lookup(name.Name); uobj != nil {\n\t\t\t\tif utn, ok := uobj.(*TypeName); ok {\n\t\t\t\t\treturn ptr, utn\n\t\t\t\t}\n\t\t\t}\n\t\t\tbreak\n\t\t}")
	src = mustReplace(f, src,
		"\t\tptr, base := check.resolveBaseTypeName(m.ptr, m.recv)\n\t\tif base != nil {\n\t\t\tm.obj.hasPtrRecv_ = ptr\n\t\t\tcheck.methods[base] = append(check.methods[base], m.obj)\n\t\t}",
		"\t\tptr, base := check.resolveBaseTypeName(m.ptr, m.recv)\n\t\tif base != nil {\n\t\t\tm.obj.hasPtrRecv_ = ptr\n\t\t\tif base.pkg == nil {\n\t\t\t\t// Moxie: a builtin receiver base - record the method on it.\n\t\t\t\tif basic, ok := base.typ.(*Basic); ok {\n\t\t\t\t\tif alt := basicAddMethod(basic, m.obj); alt != nil {\n\t\t\t\t\t\tcheck.errorf(m.obj, DuplicateMethod, \"method %s.%s already declared\", basic.name, m.obj.Name())\n\t\t\t\t\t}\n\t\t\t\t}\n\t\t\t\tcontinue\n\t\t\t}\n\t\t\tcheck.methods[base] = append(check.methods[base], m.obj)\n\t\t}")
	mustWrite(f, src)

	// universe.go: declare the builtin String() methods in the universe.
	f = filepath.Join(dir, "universe.go")
	src = mustRead(f)
	src = mustReplace(f, src,
		"\tdefPredeclaredFuncs()\n\n\tuniverseIota = Universe.Lookup(\"iota\")",
		"\tdefPredeclaredFuncs()\n\n\t// Moxie: the builtin String() methods are part of the universe.\n\tdefBuiltinStringMethods()\n\n\tuniverseIota = Universe.Lookup(\"iota\")")
	mustWrite(f, src)

	// signature.go: allow the Basic receiver base.
	f = filepath.Join(dir, "signature.go")
	src = mustRead(f)
	src = mustReplace(f, src,
		"\tcase *Basic:\n\t\tcheck.errorf(pos, InvalidRecv, \"cannot define new methods on non-local type %s\", rtyp)",
		"\tcase *Basic:\n\t\t// Moxie: a builtin type may carry methods (src/runtime/stringer.mx);\n\t\t// the method set is recorded on the type itself.")
	mustWrite(f, src)

	// methodset.go: include the builtin methods in the method set, and let a
	// byte slice report string's methods (string and []byte are one type).
	f = filepath.Join(dir, "methodset.go")
	src = mustRead(f)
	src = mustReplace(f, src,
		"\t\t\tswitch t := under(typ).(type) {\n\t\t\tcase *Struct:",
		"\t\t\t// Moxie: a builtin type may carry methods.\n\t\t\tif basic, ok := under(typ).(*Basic); ok {\n\t\t\t\tfor i := 0; i < basicNumMethods(basic); i++ {\n\t\t\t\t\tm := basicMethod(basic, i)\n\t\t\t\t\tif _, dup := mset[m.Id()]; dup {\n\t\t\t\t\t\tcontinue // the type declares its own method of that name\n\t\t\t\t\t}\n\t\t\t\t\tmset = mset.addOne(m, concat(e.index, i), e.indirect, e.multiples)\n\t\t\t\t}\n\t\t\t}\n\t\t\t// Moxie: []byte is the string type, whose method set carries String().\n\t\t\tif _, isSlice := Unalias(typ).(*Slice); isSlice && isByteSlice(typ) {\n\t\t\t\tbasic := Typ[String]\n\t\t\t\tfor i := 0; i < basicNumMethods(basic); i++ {\n\t\t\t\t\tmset = mset.addOne(basicMethod(basic, i), concat(e.index, i), e.indirect, e.multiples)\n\t\t\t\t}\n\t\t\t}\n\n\t\t\tswitch t := under(typ).(type) {\n\t\t\tcase *Struct:")
	mustWrite(f, src)

	// lookup.go: find the builtin methods when resolving a selector, and the
	// string methods for a byte slice.
	f = filepath.Join(dir, "lookup.go")
	src = mustRead(f)
	src = mustReplace(f, src,
		"\t\t\tswitch t := under(typ).(type) {\n\t\t\tcase *Struct:\n\t\t\t\t// look for a matching field and collect embedded types",
		"\t\t\t// Moxie: a builtin type may carry methods.\n\t\t\tif basic, ok := under(typ).(*Basic); ok {\n\t\t\t\tif i, m := basicLookupMethod(basic, pkg, name, foldCase); m != nil {\n\t\t\t\t\tindex = concat(e.index, i)\n\t\t\t\t\tif obj != nil || e.multiples {\n\t\t\t\t\t\treturn nil, index, false\n\t\t\t\t\t}\n\t\t\t\t\tobj = m\n\t\t\t\t\tindirect = e.indirect\n\t\t\t\t\tcontinue\n\t\t\t\t}\n\t\t\t}\n\t\t\t// Moxie: []byte is the string type, whose method set carries String().\n\t\t\tif _, isSlice := Unalias(typ).(*Slice); isSlice && isByteSlice(typ) {\n\t\t\t\tif i, m := basicLookupMethod(Typ[String], pkg, name, foldCase); m != nil {\n\t\t\t\t\tindex = concat(e.index, i)\n\t\t\t\t\tif obj != nil || e.multiples {\n\t\t\t\t\t\treturn nil, index, false\n\t\t\t\t\t}\n\t\t\t\t\tobj = m\n\t\t\t\t\tindirect = e.indirect\n\t\t\t\t\tcontinue\n\t\t\t\t}\n\t\t\t}\n\n\t\t\tswitch t := under(typ).(type) {\n\t\t\tcase *Struct:\n\t\t\t\t// look for a matching field and collect embedded types")
	mustWrite(f, src)
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
		// spawn returns the target's breaker channel: the target declares
		// (closed chan struct{}) and the caller's value must be typed as the
		// target returned it. The default (a target whose signature is not
		// available yet) is the same chan struct{}.
		fnArg := 0
		if isString(x.typ) {
			fnArg = 1
		}
		x.mode = value
		x.typ = NewChan(SendRecv, NewStruct(nil, nil))
		if fnArg < len(args) {
			if sig, ok := under(args[fnArg].typ).(*Signature); ok && sig.Results().Len() == 1 {
				x.typ = sig.Results().At(0).typ
			}
		}
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

	// append is removed outright: no spelling of it resolves, and there is no
	// internal alias left for the compiler to lower. push/pop/resize are the
	// slice mutators.
	src = mustReplace(f, src,
		"\t_Append:  {\"append\", 1, true, expression},\n",
		"")

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

	// Patch builtins.go: allow ... on push and add type-checking handlers for
	// push/pop/resize.
	f = filepath.Join(dir, "builtins.go")
	src = mustRead(f)

	src = mustReplace(f, src,
		"\t// append is the only built-in that permits the use of ... for the last argument\n\tbin := predeclaredFuncs[id]\n\tif hasDots(call) && id != _Append {",
		"\t// push is the only built-in that permits the use of ... for the last argument\n\tbin := predeclaredFuncs[id]\n\tif hasDots(call) && id != _Push {")

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
		// A type parameter constrained by ~[]byte has no *Slice under it, so
		// the slice-ness test goes through sliceElem, the same way push does:
		// time.parseNanoseconds[bytes []byte] resizes its value.
		if _, err := sliceElem(x); err == nil {
			x.mode = value
			x.typ = args[0].typ
		} else if isString(args[0].typ) {
			x.mode = value
			x.typ = args[0].typ
		} else {
			check.errorf(args[0], InvalidCall, invalidOp+"first argument to resize must be a slice, got %s", args[0])
		}
		// The length argument may be any integer type, as a slice index may be:
		// math/big passes a uint32. Untyped constants keep their untyped kind
		// and the emitter widens or truncates to the slice header's width.
		var lenTyp Type = Typ[Int32]
		if args[1].typ != nil && isInteger(args[1].typ) {
			lenTyp = args[1].typ
		}
		sig := makeSig(x.typ, x.typ, lenTyp)
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

func patchArrayCapNegative(dir string) {
	// Moxie: an array's capacity is NEGATIVE. The negative value is the sentinel
	// that says the extent cannot change (mxutil's capFixed), so a growth guard
	// answers "no room" instead of allocating, and cap(arr[:]) is the same
	// storage with the sign flipped - a slice of an array stays
	// length-changeable. len() keeps the type's length: only cap carries the
	// sentinel. See W7 in docs/PLAN_REMAINING.md.
	//
	// This has to happen here rather than in the compiler: Go makes
	// cap(anArrayVar) a CONSTANT, so go/types folds it and no lowering pass ever
	// sees a cap expression to rewrite (measured: patching the vendored go/ssa
	// had no effect for exactly this reason).
	f := filepath.Join(dir, "builtins.go")
	src := mustRead(f)
	src = mustReplace(f, src,
		"\t\t\tif !check.hasCallOrRecv {\n\t\t\t\tmode = constant_\n\t\t\t\tif t.len >= 0 {\n\t\t\t\t\tval = constant.MakeInt64(t.len)\n\t\t\t\t} else {",
		"\t\t\tif !check.hasCallOrRecv {\n\t\t\t\tmode = constant_\n\t\t\t\tif t.len >= 0 {\n\t\t\t\t\tif id == _Cap {\n\t\t\t\t\t\tval = constant.MakeInt64(-t.len)\n\t\t\t\t\t} else {\n\t\t\t\t\t\tval = constant.MakeInt64(t.len)\n\t\t\t\t\t}\n\t\t\t\t} else {")
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

// patchChanFrameBudget lets `__slicealloc(chan T, depth, bytes)` carry a second
// size argument. Moxie states a spawn channel's frame byte budget next to its
// declared depth, and that budget may be a run-time expression (relay policy
// read from config). Go's make validation allows one size argument for a
// channel and treats two as slice len/cap, which does not apply to a channel.
func patchChanFrameBudget(dir string) {
	f := filepath.Join(dir, "builtins.go")
	src := mustRead(f)

	src = mustReplace(f, src,
		"\t\tvar min int // minimum number of arguments\n\t\tswitch u.(type) {\n\t\tcase *Slice:\n\t\t\tmin = 2\n\t\tcase *Map, *Chan:\n\t\t\tmin = 1\n\t\tdefault:",
		"\t\tvar min int // minimum number of arguments\n\t\tmax := 0  // maximum number of size arguments\n\t\tswitch u.(type) {\n\t\tcase *Slice:\n\t\t\tmin = 2\n\t\t\tmax = 2\n\t\tcase *Map:\n\t\t\tmin = 1\n\t\t\tmax = 1\n\t\tcase *Chan:\n\t\t\tmin = 1\n\t\t\tmax = 2 // depth, then the frame byte budget\n\t\tdefault:")

	src = mustReplace(f, src,
		"\t\tif nargs < min || min+1 < nargs {\n\t\t\tcheck.errorf(call, WrongArgCount, invalidOp+\"%v expects %d or %d arguments; found %d\", call, min, min+1, nargs)\n\t\t\treturn\n\t\t}",
		"\t\tif nargs < min || max+1 < nargs {\n\t\t\tcheck.errorf(call, WrongArgCount, invalidOp+\"%v expects %d to %d arguments; found %d\", call, min, max+1, nargs)\n\t\t\treturn\n\t\t}")

	src = mustReplace(f, src,
		"\t\tif len(sizes) == 2 && sizes[0] > sizes[1] {",
		"\t\tif _, isSlice := u.(*Slice); isSlice && len(sizes) == 2 && sizes[0] > sizes[1] {")

	mustWrite(f, src)
}

// patchSigintBuiltin adds the __sigint builtin: the call that names the
// domain's termination channel, which the predeclared word `sigint` is lowered
// to. It is a zero-argument expression returning chan struct{} - the same handle
// on both sides, so a select arm and a direct receive see the same close.
func patchSigintBuiltin(dir string) {
	f := filepath.Join(dir, "universe.go")
	src := mustRead(f)

	src = mustReplace(f, src,
		"\t_Resize\n\n\t// package unsafe",
		"\t_Resize\n\t_Sigint\n\n\t// package unsafe")

	src = mustReplace(f, src,
		"\t_Resize:  {\"resize\", 2, false, statement},",
		"\t_Resize:  {\"resize\", 2, false, statement},\n\t_Sigint:  {\"__sigint\", 0, false, expression},")

	mustWrite(f, src)

	f = filepath.Join(dir, "builtins.go")
	src = mustRead(f)

	src = mustReplace(f, src,
		"\tcase _Recover:\n\t\t// recover() string",
		`	case _Sigint:
		// __sigint() chan struct{}: the domain's termination channel, closed
		// when the process is told to stop. The handle is the runtime's, so
		// every call names the same channel; the element is the empty struct
		// because the close is the whole message.
		if nargs != 0 {
			check.errorf(call, WrongArgCount, invalidOp+"__sigint takes no arguments")
			return
		}
		x.mode = value
		x.typ = NewChan(SendRecv, NewStruct(nil, nil))
		if check.recordTypes() {
			check.recordBuiltinType(call.Fun, makeSig(x.typ))
		}

	case _Recover:
		// recover() string`)

	mustWrite(f, src)
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "patch-gotypes: "+format+"\n", args...)
	os.Exit(1)
}
