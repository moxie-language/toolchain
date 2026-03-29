package moxie

// Spawn starts fn in a new isolated domain (OS process on native, Service
// Worker on JS). Arguments are deep-copied across the domain boundary.
// Channels passed as arguments become IPC channels.
//
// The compiler intercepts calls to this function and emits spawn-specific
// code instead of a regular function call.
func Spawn(fn interface{}, args ...interface{}) {
	// This function body is never executed — the compiler replaces the
	// call with spawn machinery. If somehow reached, panic.
	panic("moxie.Spawn: compiler failed to intercept spawn call")
}
