package main

type Point struct {
	X int32
	Y int32
}

func main() {
	// & instead of new
	p := &Point{X: 1, Y: 2}
	if p.X == 1 && p.Y == 2 {
		println("PASS: struct literal with &")
	} else {
		println("FAIL: struct literal with &")
	}

	// var + & instead of new(int32)
	var n int32 = 42
	ptr := &n
	if *ptr == 42 {
		println("PASS: var + &")
	} else {
		println("FAIL: var + &")
	}

	// Comma-separated cases instead of fallthrough
	x := int32(2)
	switch x {
	case 1, 2, 3:
		println("PASS: comma cases")
	default:
		println("FAIL: comma cases")
	}

	// All valid numeric types
	var a int8 = 1
	var b int16 = 2
	var c int32 = 3
	var d int64 = 4
	var e uint8 = 5
	var f uint16 = 6
	var g uint32 = 7
	var h uint64 = 8
	var i float32 = 9.5
	var j float64 = 10.5
	var k byte = 11
	var l bool = true
	sum := int64(a) + int64(b) + int64(c) + d + int64(e) + int64(f) + int64(g) + int64(h)
	if sum == 36 && i == 9.5 && j == 10.5 && k == 11 && l {
		println("PASS: all valid types")
	} else {
		println("FAIL: all valid types")
	}
}
