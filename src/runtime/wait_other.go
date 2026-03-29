//go:build !moxie.riscv && !cortexm && !(linux && !baremetal && !moxie.wasm && !nintendoswitch) && !darwin

package runtime

func waitForEvents() {
	runtimePanic("deadlocked: no event source")
}
