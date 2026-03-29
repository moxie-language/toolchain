package main

func wslice(x []int32) {
	if len(x) == 3 && x[0] == 10 && x[1] == 20 && x[2] == 30 {
		println("PASS:slice-int32")
	} else {
		println("FAIL:slice-int32")
	}
}

func main() {
	spawn(wslice, []int32{10, 20, 30})
	for i := int32(0); i < 50000000; i++ {
	}
}
