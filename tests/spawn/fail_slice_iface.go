package main

type Stringer interface {
	String() string
}

func wsi(x []Stringer) {
	println(len(x))
}

func main() {
	spawn(wsi, []Stringer{}) // ERROR: slice element is interface type
}
