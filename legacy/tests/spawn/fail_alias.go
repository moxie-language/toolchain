package main

func echo(x int32) {
	println(x)
}

func compute() int32 {
	return 42
}

func main() {
	x := compute()
	spawn(echo, x)
	println(x) // ERROR: variable used after spawn
}
