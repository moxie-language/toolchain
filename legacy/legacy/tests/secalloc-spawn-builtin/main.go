package main

import (
	"os"
	"runtime"
	"unsafe"
)

// Milestone-3 verification: first-class spawn lockdown channel.
//
// The parent uses the actual `spawn` keyword (not bare fork) to create
// a child domain. spawnDomain creates the lockdown pipe and routes the
// child's secalloc handler to the write end automatically — no manual
// SetSecureLockdownFd call in user code. The parent retrieves the read
// fd via runtime.LastSpawnedLockdownFd().
//
// The parent's read on the lockdown fd is what synchronizes with the
// child: it blocks until the child writes the marker (on guard-page
// fault) or the pipe closes (if the child exits without faulting).
//
// Verifies:
//   - parent stdout contains LOCKDOWN_VIA_SPAWN_RECEIVED
//   - the raw secret pattern does not appear anywhere
//   - parent stderr does not contain MOXIE_SECALLOC_LOCKDOWN
//     (the marker should have travelled via the inherited pipe, not stderr)

func childDomain() {
	os.Stdout.Write([]byte("CHILD_START\n"))
	secret := []byte{:32, secure}
	os.Stdout.Write([]byte("CHILD_ALLOCATED\n"))
	pattern := []byte("MOXIE_SECRET_PAYLOAD_32_BYTES_AA")
	copy(secret, pattern)
	os.Stdout.Write([]byte("CHILD_COPIED\n"))

	// Trip the trailing guard page exactly as the M1/M2 tests do.
	ptr := (*byte)(unsafe.Add(unsafe.Pointer(&secret[0]), 4096))
	sink := *ptr
	os.Stdout.Write([]byte{sink})
	os.Stdout.Write([]byte("UNREACHABLE\n"))
}

func main() {
	// spawn returns a chan struct{} lifecycle handle. Bind it to a
	// typed local — discarding directly with `_ =` triggers an SSA
	// builder mismatch in the spawn intrinsic.
	done := spawn(childDomain)
	_ = done

	fd := runtime.LastSpawnedLockdownFd()
	if fd < 0 {
		os.Stderr.Write([]byte("FAIL: spawn did not provide a lockdown fd\n"))
		os.Exit(1)
	}

	rf := os.NewFile(uintptr(fd), "lockdown")
	buf := make([]byte, 64)
	n, _ := rf.Read(buf)

	if n <= 0 {
		os.Stderr.Write([]byte("FAIL: lockdown pipe yielded no bytes\n"))
		os.Exit(1)
	}

	marker := []byte("MOXIE_SECALLOC_LOCKDOWN")
	got := buf[:n]
	found := false
	for i := 0; i+len(marker) <= len(got); i++ {
		eq := true
		for j := 0; j < len(marker); j++ {
			if got[i+j] != marker[j] {
				eq = false
				break
			}
		}
		if eq {
			found = true
			break
		}
	}

	if found {
		os.Stdout.Write([]byte("LOCKDOWN_VIA_SPAWN_RECEIVED\n"))
	} else {
		os.Stderr.Write([]byte("FAIL: pipe data missing marker\n"))
		os.Exit(1)
	}
}
