package main

func wmulti(a int32, b float64, c bool, d uint8) {
	if a == 42 && b == 3.14 && c && d == 7 {
		println("PASS:multi-params")
	} else {
		println("FAIL:multi-params")
	}
}

func main() {
	spawn(wmulti, int32(42), float64(3.14), true, uint8(7))
	for i := int32(0); i < 50000000; i++ {
	}
}
