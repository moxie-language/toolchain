package main

func wf32(x float32) {
	if x == 1.5 {
		println("PASS:float32")
	} else {
		println("FAIL:float32")
	}
}

func wf64(x float64) {
	if x == 2.75 {
		println("PASS:float64")
	} else {
		println("FAIL:float64")
	}
}

func main() {
	spawn(wf32, float32(1.5))
	spawn(wf64, float64(2.75))
	for i := int32(0); i < 50000000; i++ {
	}
}
