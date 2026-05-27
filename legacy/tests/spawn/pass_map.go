package main

func wmap(x map[string]int32) {
	if x["a"] == 1 && x["b"] == 2 {
		println("PASS:map-string-int32")
	} else {
		println("FAIL:map-string-int32")
	}
}

func main() {
	spawn(wmap, map[string]int32{"a": 1, "b": 2})
	for i := int32(0); i < 50000000; i++ {
	}
}
