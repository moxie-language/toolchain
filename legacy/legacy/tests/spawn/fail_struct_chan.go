package main

type BadStruct struct {
	Ch chan int32
	X  int32
}

func wbad(s BadStruct) {
	println(s.X)
}

func main() {
	spawn(wbad, BadStruct{X: 1}) // ERROR: nested channel in struct
}
