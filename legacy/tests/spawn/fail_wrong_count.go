package main

func worker(x int32, y int32) {
	println(x, y)
}

func main() {
	spawn(worker, int32(1)) // ERROR: expects 2 arguments, got 1
}
