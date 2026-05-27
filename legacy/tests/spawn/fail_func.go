package main

func wfn(f func()) {
	f()
}

func main() {
	spawn(wfn, func() { println("bad") }) // ERROR: function type
}
