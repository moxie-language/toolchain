package main

import (
	"os"
	"runtime"
	"unsafe"
)

// Milestone-2 verification for the spawn-inherited lockdown channel.
//
// Layout:
//
//   1. Parent creates a unidirectional pipe via runtime.MakePipe.
//   2. Parent forks. The child inherits the pipe fds (fork copies the fd table).
//   3. Child closes the read end, calls runtime.SetSecureLockdownFd(write),
//      allocates a guarded arena, writes a recognizable secret pattern, then
//      deliberately reads past the guard page to trip SIGSEGV. The signal
//      handler wipes the arena and writes MOXIE_SECALLOC_LOCKDOWN to the
//      write fd of the pipe — NOT to stderr.
//   4. Parent closes the write end, reads from the read end, waits for the
//      child to die, and prints LOCKDOWN_RECEIVED if the marker arrived via
//      the inherited pipe.
//
// The harness verifies:
//   - exit status of the parent is 0
//   - parent stdout contains LOCKDOWN_RECEIVED (so the byte traversed the pipe)
//   - parent stdout contains CHILD_EXITED_SIGSEGV (child died on the guard page)
//   - the raw secret pattern does NOT appear anywhere in parent stdout/stderr
//
// Why bare fork instead of spawn: spawn requires moxie.Codec types and a
// dispatch loop. M2.2 is about verifying the lockdown fd is routable to a
// real cross-process channel, not about exercising spawn. Once the routing
// is proven, integration with spawn is a separate, smaller step.

func main() {
	read, write := runtime.MakePipe()
	if read < 0 || write < 0 {
		os.Stderr.Write([]byte("FAIL: MakePipe failed\n"))
		os.Exit(1)
	}

	pid := runtime.Fork()
	if pid < 0 {
		os.Stderr.Write([]byte("FAIL: Fork failed\n"))
		os.Exit(1)
	}

	if pid == 0 {
		// Child path.
		runtime.Close(read)
		runtime.SetSecureLockdownFd(write)

		secret := []byte{:32, secure}
		pattern := []byte("MOXIE_SECRET_PAYLOAD_32_BYTES_AA")
		copy(secret, pattern)

		// Trip the trailing guard page exactly as the M1 test does.
		ptr := (*byte)(unsafe.Add(unsafe.Pointer(&secret[0]), 4096))
		sink := *ptr
		os.Stdout.Write([]byte{sink})
		os.Stdout.Write([]byte("UNREACHABLE\n"))
		os.Exit(0)
	}

	// Parent path.
	runtime.Close(write)

	// Read up to 64 bytes from the lockdown pipe. The marker is
	// "MOXIE_SECALLOC_LOCKDOWN\n" (24 bytes).
	rf := os.NewFile(uintptr(read), "lockdown")
	buf := []byte{:64}
	n, _ := rf.Read(buf)

	status := runtime.Waitpid(pid)

	if n > 0 {
		// Look for the marker substring in what we received.
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
			os.Stdout.Write([]byte("LOCKDOWN_RECEIVED\n"))
		} else {
			os.Stdout.Write([]byte("FAIL: pipe data did not contain marker\n"))
		}
	} else {
		os.Stdout.Write([]byte("FAIL: pipe yielded no bytes\n"))
	}

	// status is the raw waitpid value: low 7 bits = signal if signalled,
	// bit 7 = core dump flag, bits 8-15 = exit status if exited normally.
	// SIGSEGV = 11. We just check the low 7 bits.
	if status >= 0 && (status&0x7f) == 11 {
		os.Stdout.Write([]byte("CHILD_EXITED_SIGSEGV\n"))
	} else {
		os.Stdout.Write([]byte("FAIL: child did not die on SIGSEGV (status="))
		writeInt(status)
		os.Stdout.Write([]byte(")\n"))
	}
}

// writeInt writes a small int as decimal to stdout without using fmt
// (which we deliberately avoid here to keep the test minimal).
func writeInt(n int32) {
	if n == 0 {
		os.Stdout.Write([]byte("0"))
		return
	}
	if n < 0 {
		os.Stdout.Write([]byte("-"))
		n = -n
	}
	var buf [12]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	os.Stdout.Write(buf[i:])
}
