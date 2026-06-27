package main

import (
	"fmt"
	"strings"
	"sync"
)

type Config struct {
	Name    string
	Count   int
	Values  map[string]interface{}
	mu      sync.Mutex
}

var defaultConfig = &Config{
	Name:  "default",
	Count: 100,
}

func init() {
	defaultConfig.Values = make(map[string]interface{})
}

func init() {
	fmt.Println("second init")
}

func main() {
	cfg := defaultConfig
	cfg.mu.Lock()
	defer cfg.mu.Unlock()

	ch := make(chan int)
	go func() {
		for i := 0; i < 10; i++ {
			ch <- i * i
		}
		close(ch)
	}()

	results := make([]int, 0, 20)
	for v := range ch {
		results = append(results, v)
	}

	msg := "results: " + fmt.Sprint(results)
	fmt.Println(msg)

	switch v := cfg.Count; {
	case v < 10:
		fmt.Println("small")
	case v < 100:
		fallthrough
	case v < 1000:
		fmt.Println("medium")
	default:
		fmt.Println("large")
	}

	if s, ok := cfg.Values["key"].(string); ok {
		fmt.Println(strings.ToUpper(s))
	}

	var total uint
	for _, r := range results {
		total += uint(r)
	}
	fmt.Printf("total: %d\n", total)
}

func process(data []byte, n int) []byte {
	out := new(bytes.Buffer)
	for i := 0; i < n; i++ {
		out.Write(data)
	}
	return out.Bytes()
}
