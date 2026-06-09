package main

func wptr(x *int32) {
	println(*x)
}

func main() {
	v := int32(42)
	spawn(wptr, &v) // ERROR: pointer type
}
