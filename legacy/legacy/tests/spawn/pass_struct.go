package main

type Point struct {
	X int32
	Y int32
}

type Mixed struct {
	A float64
	B int32
	C bool
	D uint8
}

type Nested struct {
	P Point
	V int64
}

func wpoint(p Point) {
	if p.X == 10 && p.Y == 20 {
		println("PASS:struct-point")
	} else {
		println("FAIL:struct-point")
	}
}

func wmixed(m Mixed) {
	if m.A == 3.5 && m.B == 99 && m.C && m.D == 7 {
		println("PASS:struct-mixed")
	} else {
		println("FAIL:struct-mixed")
	}
}

func wnested(n Nested) {
	if n.P.X == 1 && n.P.Y == 2 && n.V == 42 {
		println("PASS:struct-nested")
	} else {
		println("FAIL:struct-nested")
	}
}

func main() {
	spawn(wpoint, Point{X: 10, Y: 20})
	spawn(wmixed, Mixed{A: 3.5, B: 99, C: true, D: 7})
	spawn(wnested, Nested{P: Point{X: 1, Y: 2}, V: 42})
	for i := int32(0); i < 50000000; i++ {
	}
}
