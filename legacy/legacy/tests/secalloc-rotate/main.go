package main

import (
	"os"
	"runtime"
	"unsafe"
)

// Milestone-3.2 verification: SecureRotate.
//
// Allocates a secret, rotates it, verifies:
//   - the rotated slice holds the same bytes
//   - the rotated slice has a different base address (fresh mapping)
//   - the old base is unmapped — reading it faults, which triggers the
//     standard wipe-and-die path
//
// Layout:
//   parent stdout  — normal progress markers
//   parent stderr  — MOXIE_SECALLOC_LOCKDOWN when the old-base read faults
//
// Harness verifies: process dies with a SIGSEGV after printing
// ROTATED_OK, and the raw secret pattern never appears anywhere.

func main() {
	secret := []byte{:32, secure}
	pattern := []byte("MOXIE_SECRET_PAYLOAD_32_BYTES_AA")
	copy(secret, pattern)

	oldBase := unsafe.Pointer(&secret[0])
	os.Stdout.Write([]byte("BEFORE_ROTATE\n"))

	rotated := runtime.SecureRotate(secret)
	newBase := unsafe.Pointer(&rotated[0])

	if newBase == oldBase {
		os.Stderr.Write([]byte("FAIL: rotated slice reused old base\n"))
		os.Exit(1)
	}

	// Verify contents survived the copy.
	for i := 0; i < 32; i++ {
		if rotated[i] != pattern[i] {
			os.Stderr.Write([]byte("FAIL: rotated contents differ\n"))
			os.Exit(1)
		}
	}
	os.Stdout.Write([]byte("ROTATED_OK\n"))

	// Rotate several more times to exercise the C-side slot reuse path.
	// Without slot reuse the fixed 64-slot registry would fill up quickly.
	live := rotated
	for i := 0; i < 10; i++ {
		live = runtime.SecureRotate(live)
		if live[0] != pattern[0] || live[31] != pattern[31] {
			os.Stderr.Write([]byte("FAIL: multi-rotate corrupted contents\n"))
			os.Exit(1)
		}
	}
	os.Stdout.Write([]byte("MULTI_ROTATED_OK\n"))

	// Touch the old mapping. It should be unmapped — reading it faults.
	// The signal handler wipes the (new) arena and prints the lockdown
	// marker, then the process dies.
	orphan := (*byte)(oldBase)
	sink := *orphan
	os.Stdout.Write([]byte{sink})

	os.Stdout.Write([]byte("UNREACHABLE\n"))
}
