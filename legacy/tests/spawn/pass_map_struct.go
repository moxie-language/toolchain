package main

type MapStruct struct {
	M map[string]int32
}

func makeMapStruct() (sv MapStruct) {
	sv = MapStruct{M: map[string]int32{"x": 42, "y": 99}}
	return
}

func makeMapStructPtr() (sp *MapStruct) {
	sp = &MapStruct{M: map[string]int32{"p": 100, "q": 200}}
	return
}

func main() {
	sv := makeMapStruct()
	if sv.M["x"] == 42 && sv.M["y"] == 99 {
		println("PASS:map-struct-value-return")
	} else {
		println("FAIL:map-struct-value-return")
	}

	sp := makeMapStructPtr()
	if sp.M["p"] == 100 && sp.M["q"] == 200 {
		println("PASS:map-struct-ptr-return")
	} else {
		println("FAIL:map-struct-ptr-return")
	}
}
