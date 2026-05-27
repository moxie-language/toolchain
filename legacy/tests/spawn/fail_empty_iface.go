package main

func wany(x interface{}) {
	println(x)
}

func main() {
	spawn(wany, "hello") // ERROR: interface type (even empty interface)
}
