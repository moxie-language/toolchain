package main

func worker(x int32) {
	println(x)
}

func main() {
	spawn(worker, float64(3.14)) // ERROR: type mismatch, expects int32
}
