package main

func main() {
	var z complex64 = 1 + 2i // ERROR: complex64 not allowed
	_ = z
}
