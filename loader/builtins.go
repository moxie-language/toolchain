package loader

import (
	"go/parser"
)

// spawnBuiltinSource is injected into every user package that is the main
// package. It declares 'spawn' as a package-level function so that user
// code can call spawn(fn, args...) without importing anything.
//
// The function body is never executed — the compiler intercepts spawn
// calls at the SSA level and replaces them with domain creation code.
const spawnBuiltinSource = `package main

// spawn creates a new isolated domain (OS process on native, Service Worker
// on JS) that runs fn with the given arguments. Returns a lifecycle channel
// that closes when the child domain exits. Close it to signal the child to stop.
//
// The first argument may be a transport string:
//   spawn(fn, args...)             — local fork+socketpair (default)
//   spawn("local", fn, args...)    — explicit local
//   spawn("pipe", fn, args...)     — alias for local
//   spawn("tcp://...", fn, args...)— network transport (not yet implemented)
func spawn(fn interface{}, args ...interface{}) chan struct{} {
	panic("spawn: compiler failed to intercept")
}
`

// injectSpawnBuiltin adds the spawn function declaration to the main package.
func (p *Program) injectSpawnBuiltin() {
	main := p.MainPkg()
	if main == nil || len(main.Files) == 0 {
		return
	}
	f, err := parser.ParseFile(p.fset, "<moxie:spawn>", spawnBuiltinSource, 0)
	if err != nil {
		return // should never happen — source is a constant
	}
	main.Files = append(main.Files, f)
}
