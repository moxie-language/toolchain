package main

func wbool(x bool) {
	if x {
		println("PASS:bool")
	} else {
		println("FAIL:bool")
	}
}

func wstring(x string) {
	if x == "hello moxie" {
		println("PASS:string")
	} else {
		println("FAIL:string")
	}
}

func wbyte(x byte) {
	if x == 0xFF {
		println("PASS:byte")
	} else {
		println("FAIL:byte")
	}
}

func wrune(x rune) {
	if x == 0x1F600 {
		println("PASS:rune")
	} else {
		println("FAIL:rune")
	}
}

func main() {
	b := bool(true)
	s := string("hello moxie")
	spawn(wbool, b)
	spawn(wstring, s)
	spawn(wbyte, byte(0xFF))
	spawn(wrune, rune(0x1F600))
	for i := int32(0); i < 50000000; i++ {
	}
}
