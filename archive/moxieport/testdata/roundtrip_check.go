package main

import "fmt"

func add(a, b int) int {
	return a + b
}

func greet(name string) string {
	return "hello " + name
}

func main() {
	x := add(1, 2)
	msg := greet("world")
	fmt.Println(x, msg)
}
