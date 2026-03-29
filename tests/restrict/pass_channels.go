package main

func main() {
	// Channels work within a domain
	ch := make(chan int32, 1)
	ch <- 42
	v := <-ch
	if v == 42 {
		println("PASS: buffered channel")
	} else {
		println("FAIL: buffered channel")
	}

	// Select statement
	ch2 := make(chan int32, 1)
	ch2 <- 99
	select {
	case val := <-ch2:
		if val == 99 {
			println("PASS: select")
		} else {
			println("FAIL: select")
		}
	default:
		println("FAIL: select took default")
	}
}
