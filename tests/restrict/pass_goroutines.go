package main

func main() {
	done := make(chan int32, 1)
	go func() {
		done <- 77
	}()
	v := <-done
	if v == 77 {
		println("PASS: goroutine")
	} else {
		println("FAIL: goroutine")
	}
}
