package main

func warrchan(a [2]chan int32) {
	println("should not reach")
}

func main() {
	spawn(warrchan, [2]chan int32{}) // ERROR: nested channel in array
}
