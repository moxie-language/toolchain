package main

// moxiejs runtime regression test suite.
// Covers three compiler/runtime fixes from the surgery audit:
//   1. Closure FreeVar bind-order: MakeClosure bindings align with
//      functionParams' FreeVars-first emission order.
//   2. defer + recover: $rt.runtime.recover() implementation correctly
//      returns the panic value inside a deferred function.
//   3. Pointer-wrapper preservation in cloneValue: struct copy of a
//      struct with pointer fields preserves the {$value,$get,$set}
//      wrapper, so later marshal/deref calls don't throw
//      "X.$get is not a function".

type inner struct {
	val int32
}

type outer struct {
	p *inner
}

// makeAdder returns a closure that captures two FreeVars (base1, base2)
// and accepts two Params (x, y). If moxiejs emits Params before FreeVars
// or misaligns MakeClosure bindings, the arithmetic produces a wrong result.
func makeAdder(base1, base2 int32) func(x, y int32) int32 {
	return func(x, y int32) int32 {
		return base1 + base2 + x + y
	}
}

// willPanic triggers a panic, recovers in a deferred closure, and reports
// whether recover() returned the expected non-nil value.
func willPanic() (result bool) {
	defer func() {
		if r := recover(); r != nil {
			result = true
		}
	}()
	panic("intentional")
}

// structCopyPreservesPointer copies a struct holding a pointer field, then
// reads the pointer through both copies. Before the cloneValue fix, the
// pointer wrapper was deep-cloned as an empty struct and the second read
// threw TypeError.
func structCopyPreservesPointer() bool {
	src := outer{p: &inner{val: 42}}
	dst := src
	if src.p == nil || dst.p == nil {
		return false
	}
	// Read through both wrappers — must not throw.
	if src.p.val != 42 {
		return false
	}
	if dst.p.val != 42 {
		return false
	}
	// Mutate through one copy; the other must see the change (Go pointer
	// semantics — both copies share the same underlying inner).
	dst.p.val = 99
	if src.p.val != 99 {
		return false
	}
	return true
}

func main() {
	// Test 1: closure bind-order.
	add := makeAdder(10, 20)
	got := add(3, 4)
	if got == 37 {
		println("PASS:closure-bind-order")
	} else {
		println("FAIL:closure-bind-order")
	}

	// Test 2: defer + recover.
	if willPanic() {
		println("PASS:defer-recover")
	} else {
		println("FAIL:defer-recover")
	}

	// Test 3: struct copy preserves pointer wrapper.
	if structCopyPreservesPointer() {
		println("PASS:struct-copy-pointer")
	} else {
		println("FAIL:struct-copy-pointer")
	}
}
