package main

import (
	"os"
	"unsafe"
)

// Milestone-2 verification for the secure allocator:
//
//   1. Allocate a 32-byte guarded region via the `[]byte{:32, secure}`
//      literal syntax. The mxtext rewriter turns this into a call to
//      __moxie_secalloc(32), which the SSA lowering pass intercepts and
//      replaces with a runtime.SecureAlloc call.
//   2. Write a recognizable pattern — the "secret".
//   3. Print WROTE_SECRET so the test harness can sequence expectations.
//   4. Deliberately read one byte past the trailing guard page via pointer
//      arithmetic. That triggers SIGSEGV.
//   5. runtime's signal handler calls into secalloc.c which synchronously
//      wipes the arena with noise and writes MOXIE_SECALLOC_LOCKDOWN to
//      stderr, then the process dies with SIGSEGV.
//
// The harness verifies:
//   - exit status is SIGSEGV (signalled death, not a clean exit)
//   - stdout contains WROTE_SECRET
//   - stderr contains MOXIE_SECALLOC_LOCKDOWN
//   - stdout/stderr do NOT contain the raw secret pattern anywhere
func main() {
	secret := []byte{:32, secure}

	pattern := []byte("MOXIE_SECRET_PAYLOAD_32_BYTES_AA")
	copy(secret, pattern)

	os.Stdout.Write([]byte("WROTE_SECRET\n"))

	// Jump 4096 bytes past secret[0]. Secure arenas round the data region
	// up to a whole page, so the first out-of-bounds offset that is
	// guaranteed to sit in the trailing PROT_NONE guard page is +pageSize
	// from the base. The read faults synchronously.
	ptr := (*byte)(unsafe.Add(unsafe.Pointer(&secret[0]), 4096))
	sink := *ptr
	os.Stdout.Write([]byte{sink})

	os.Stdout.Write([]byte("UNREACHABLE\n"))
}
