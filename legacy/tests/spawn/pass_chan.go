package main

func wchan(ch chan int32) {
	// Channel is nil for now (IPC not wired yet).
	// Just verify the param arrives without crashing.
	println("PASS:chan-param")
}

func main() {
	ch := make(chan int32)
	spawn(wchan, ch)
	for i := int32(0); i < 50000000; i++ {
	}
}
