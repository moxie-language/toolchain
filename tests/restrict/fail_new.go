package main

func main() {
	p := new(int32) // ERROR: new not allowed
	_ = p
}
