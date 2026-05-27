package main

func wi8(x int8) {
	if x == 42 {
		println("PASS:int8")
	} else {
		println("FAIL:int8")
	}
}

func wi16(x int16) {
	if x == 1000 {
		println("PASS:int16")
	} else {
		println("FAIL:int16")
	}
}

func wi32(x int32) {
	if x == 100000 {
		println("PASS:int32")
	} else {
		println("FAIL:int32")
	}
}

func wi64(x int64) {
	if x == 9000000000 {
		println("PASS:int64")
	} else {
		println("FAIL:int64")
	}
}

func wu8(x uint8) {
	if x == 255 {
		println("PASS:uint8")
	} else {
		println("FAIL:uint8")
	}
}

func wu16(x uint16) {
	if x == 65000 {
		println("PASS:uint16")
	} else {
		println("FAIL:uint16")
	}
}

func wu32(x uint32) {
	if x == 3000000000 {
		println("PASS:uint32")
	} else {
		println("FAIL:uint32")
	}
}

func wu64(x uint64) {
	if x == 18000000000000000000 {
		println("PASS:uint64")
	} else {
		println("FAIL:uint64")
	}
}

func main() {
	spawn(wi8, int8(42))
	spawn(wi16, int16(1000))
	spawn(wi32, int32(100000))
	spawn(wi64, int64(9000000000))
	spawn(wu8, uint8(255))
	spawn(wu16, uint16(65000))
	spawn(wu32, uint32(3000000000))
	spawn(wu64, uint64(18000000000000000000))
	for i := int32(0); i < 50000000; i++ {
	}
}
