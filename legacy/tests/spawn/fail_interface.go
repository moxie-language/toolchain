package main

type Stringer interface {
	String() string
}

func wiface(x Stringer) {
	println(x.String())
}

func main() {
	spawn(wiface, nil) // ERROR: interface type
}
