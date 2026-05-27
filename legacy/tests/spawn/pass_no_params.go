package main

func wnone() {
	println("PASS:no-params")
}

func main() {
	spawn(wnone)
	for i := int32(0); i < 50000000; i++ {
	}
}
