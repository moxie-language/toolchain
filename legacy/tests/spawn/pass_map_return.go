package main

func makeMap() (m map[string]int32) {
	m = map[string]int32{}
	m["a"] = 1
	m["b"] = 2
	m["c"] = 3
	return
}

func main() {
	m := makeMap()
	if m["a"] == 1 && m["b"] == 2 && m["c"] == 3 {
		println("PASS:map-direct-return")
	} else {
		println("FAIL:map-direct-return")
	}
}
