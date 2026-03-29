package main

func main() {
	x := int32(1)
	switch x {
	case 1:
		println("one")
		fallthrough // ERROR: fallthrough not allowed
	case 2:
		println("two")
	}
}
