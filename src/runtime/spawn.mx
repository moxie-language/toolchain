//go:build linux

package runtime

import "unsafe"

// spawnDomain creates a new isolated domain by forking the current process.
// The child process executes fn(args) with its own cooperative scheduler.
// Communication between parent and child happens over a Unix socketpair.
func spawnDomain(fn uintptr, args unsafe.Pointer) {
	// Create a socketpair for IPC between parent and child.
	var fds [2]int32
	r := moxie_socketpair(1, 1, 0, unsafe.Pointer(&fds[0])) // AF_UNIX=1, SOCK_STREAM=1
	if r < 0 {
		runtimePanic("spawn: socketpair failed")
	}

	pid := moxie_fork()
	if pid < 0 {
		runtimePanic("spawn: fork failed")
	}

	if pid == 0 {
		// Child process.
		moxie_close(fds[0]) // close parent's end

		// Reset the scheduler — drain inherited parent task queue.
		for runqueue.Pop() != nil {
		}
		sleepQueue = nil

		// Start the spawned function as a task on a fresh scheduler.
		spawnTaskStart(fn, args, 8192)

		// Run ONLY the scheduler loop (not run() which calls main again).
		// returnAtDeadlock=true makes it return when no tasks remain.
		scheduler(true)

		// Exit child process.
		exit(0)
	}

	// Parent process.
	moxie_close(fds[1]) // close child's end

	domainRegister(pid, fds[0])
}

//go:linkname spawnTaskStart internal/task.start
func spawnTaskStart(fn uintptr, args unsafe.Pointer, stackSize uintptr)

// C functions defined in spawn_unix.c

//export moxie_fork
func moxie_fork() int32

//export moxie_close
func moxie_close(fd int32) int32

//export moxie_socketpair
func moxie_socketpair(domain, typ, protocol int32, fds unsafe.Pointer) int32
