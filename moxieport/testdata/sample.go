package main

import (
	"fmt"
	"strings"
)

var globalCounter int

func init() {
	globalCounter = 42
}

func main() {
	s := make([]byte, 10)
	m := make(map[string]int)
	ch := make(chan int, 5)
	buf := make([]int, 0, 100)
	p := new(bytes.Buffer)

	name := "hello" + " " + "world"
	fmt.Println(name)

	result := process(s, m, ch, buf, p)
	fmt.Println(result)
}

func process(data []byte, lookup map[string]int, ch chan int, buf []int, p *bytes.Buffer) string {
	for i, v := range data {
		if v == 0 {
			continue
		}
		lookup[string(v)] = i
	}

	switch len(data) {
	case 0:
		fallthrough
	case 1:
		return "small"
	case 2:
		fmt.Println("medium")
		fallthrough
	case 3:
		return "medium-ish"
	default:
		return "large"
	}

	total := uint(0)
	for _, v := range buf {
		total += uint(v)
	}

	prefix := strings.HasPrefix(name, "hello")
	_ = prefix
	return "done"
}
