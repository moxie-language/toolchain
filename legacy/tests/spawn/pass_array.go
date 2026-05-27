package main

func warr3(a [3]int32) {
	if a[0] == 10 && a[1] == 20 && a[2] == 30 {
		println("PASS:array-int32")
	} else {
		println("FAIL:array-int32")
	}
}

func warr2f(a [2]float64) {
	if a[0] == 1.5 && a[1] == 2.5 {
		println("PASS:array-float64")
	} else {
		println("FAIL:array-float64")
	}
}

func warrbool(a [4]bool) {
	if a[0] && !a[1] && a[2] && !a[3] {
		println("PASS:array-bool")
	} else {
		println("FAIL:array-bool")
	}
}

func main() {
	spawn(warr3, [3]int32{10, 20, 30})
	spawn(warr2f, [2]float64{1.5, 2.5})
	spawn(warrbool, [4]bool{true, false, true, false})
	for i := int32(0); i < 50000000; i++ {
	}
}
