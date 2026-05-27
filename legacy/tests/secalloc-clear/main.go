package main

import (
	"os"
	"runtime"
)

// Milestone-3.2b verification: SecureClear + SecureLockdown + SecureRekey.
//
// Exercises the three manual activation primitives that complement the
// fault-triggered wipe path:
//
//   SecureClear    — targeted wipe of one buffer (context-change usage)
//   SecureLockdown — global wipe of all registered arenas, without death
//   SecureRekey    — regenerate the shared noise buffer
//
// This program must exit CLEANLY (rc=0) — unlike the rotate test it does
// not intentionally fault. It proves that manual lockdown returns to the
// caller rather than delegating to the fatal-signal teardown.
//
// Verifies:
//   - SecureClear overwrites the target buffer with noise (pattern gone)
//   - SecureLockdown overwrites arenas AND writes the lockdown marker
//   - Two consecutive lockdowns produce DIFFERENT wipe bytes, proving
//     the post-lockdown rekey fired
//   - An explicit SecureRekey between lockdowns also changes the noise
//   - The raw secret pattern never leaks to stdout/stderr
//   - The process stays alive through all three lockdowns

func main() {
	secret := []byte{:32, secure}
	pattern := []byte("MOXIE_SECRET_PAYLOAD_32_BYTES_AA")
	copy(secret, pattern)
	os.Stdout.Write([]byte("BEFORE_CLEAR\n"))

	runtime.SecureClear(secret)
	if bytesEqual(secret, pattern) {
		os.Stderr.Write([]byte("FAIL: SecureClear did not overwrite\n"))
		os.Exit(1)
	}
	os.Stdout.Write([]byte("CLEARED_OK\n"))

	// First lockdown — wipes with noise N1, then rekeys to N2.
	copy(secret, pattern)
	runtime.SecureLockdown()
	if bytesEqual(secret, pattern) {
		os.Stderr.Write([]byte("FAIL: SecureLockdown did not overwrite\n"))
		os.Exit(1)
	}
	wipe1 := []byte{:32}
	copy(wipe1, secret)
	os.Stdout.Write([]byte("LOCKDOWN_1_OK\n"))

	// Second lockdown — wipes with N2, then rekeys to N3. wipe2 should
	// differ from wipe1 because the noise changed between them.
	copy(secret, pattern)
	runtime.SecureLockdown()
	wipe2 := []byte{:32}
	copy(wipe2, secret)
	os.Stdout.Write([]byte("LOCKDOWN_2_OK\n"))

	if bytesEqual(wipe1, wipe2) {
		os.Stderr.Write([]byte("FAIL: noise did not rekey between lockdowns\n"))
		os.Exit(1)
	}
	os.Stdout.Write([]byte("REKEY_OK\n"))

	// Explicit SecureRekey — should also change the noise.
	runtime.SecureRekey()
	copy(secret, pattern)
	runtime.SecureLockdown()
	wipe3 := []byte{:32}
	copy(wipe3, secret)

	if bytesEqual(wipe3, wipe2) {
		os.Stderr.Write([]byte("FAIL: explicit SecureRekey did not change noise\n"))
		os.Exit(1)
	}
	os.Stdout.Write([]byte("EXPLICIT_REKEY_OK\n"))

	os.Stdout.Write([]byte("LOCKDOWN_SURVIVED\n"))
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
