//go:build linux || darwin

package runtime

// Domain registry — tracks child domains spawned by this process.
// Single-threaded cooperative scheduler, so no lock needed.

type domainEntry struct {
	pid      int32
	parentFd int32 // parent's end of socketpair
	next     *domainEntry
}

var domainList *domainEntry

func domainRegister(pid, fd int32) {
	domainList = &domainEntry{pid: pid, parentFd: fd, next: domainList}
}

func domainCount() int {
	n := 0
	for e := domainList; e != nil; e = e.next {
		n++
	}
	return n
}

func domainLookupByPid(pid int32) *domainEntry {
	for e := domainList; e != nil; e = e.next {
		if e.pid == pid {
			return e
		}
	}
	return nil
}

func domainLookupByFd(fd int32) *domainEntry {
	for e := domainList; e != nil; e = e.next {
		if e.parentFd == fd {
			return e
		}
	}
	return nil
}
