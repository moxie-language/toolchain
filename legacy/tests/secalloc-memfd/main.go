package main

import (
	"bytes"
	"os"
	"strconv"
	"unsafe"
)

// Milestone-4 verification: memfd_secret(2) upgrade path.
//
// On Linux ≥5.14 secureMap attempts to replace the anonymous data pages of a
// secure arena with pages backed by memfd_secret(2). This test observes the
// outcome by parsing /proc/self/maps: each mapping line starts with the VA
// range (hex-hex) followed by protection, offset, device, inode, and the
// pathname. memfd_secret mappings show as "/memfd:secretmem (deleted)" in
// the pathname — deleted because we close the fd right after mmap to keep
// the mapping alive without retaining an fd table entry.
//
// Expected outcomes:
//   - Kernel ≥5.14 with secretmem enabled: SECRETMEM_OK on stdout.
//   - Older kernel or Darwin: ANONYMOUS_FALLBACK on stdout. (Still secure via
//     mlock + guard pages.)
//
// The test runner decides which outcome to assert based on the host kernel.

func main() {
	secret := []byte{:32, secure}
	copy(secret, []byte("MOXIE_M4_SECRETMEM_PROBE_BYTES_A"))

	addr := uintptr(unsafe.Pointer(&secret[0]))
	os.Stdout.Write([]byte("ADDR="))
	os.Stdout.Write([]byte(strconv.FormatUint(uint64(addr), 16)))
	os.Stdout.Write([]byte("\n"))

	maps, err := os.ReadFile("/proc/self/maps")
	if err != nil {
		os.Stderr.Write([]byte("FAIL: could not read /proc/self/maps: "))
		os.Stderr.Write([]byte(err.Error()))
		os.Stderr.Write([]byte("\n"))
		os.Exit(1)
	}

	var matched []byte
	for _, line := range bytes.Split(maps, []byte("\n")) {
		dash := bytes.IndexByte(line, '-')
		if dash <= 0 {
			continue
		}
		space := bytes.IndexByte(line, ' ')
		if space <= dash+1 {
			continue
		}
		start, errS := strconv.ParseUint(string(line[:dash]), 16, 64)
		if errS != nil {
			continue
		}
		end, errE := strconv.ParseUint(string(line[dash+1:space]), 16, 64)
		if errE != nil {
			continue
		}
		if uint64(addr) >= start && uint64(addr) < end {
			matched = line
			break
		}
	}

	if matched == nil {
		os.Stderr.Write([]byte("FAIL: arena VA not found in /proc/self/maps\n"))
		os.Exit(1)
	}

	os.Stdout.Write([]byte("MATCH="))
	os.Stdout.Write(matched)
	os.Stdout.Write([]byte("\n"))

	if bytes.Contains(matched, []byte("secretmem")) {
		os.Stdout.Write([]byte("SECRETMEM_OK\n"))
	} else {
		os.Stdout.Write([]byte("ANONYMOUS_FALLBACK\n"))
	}
}
