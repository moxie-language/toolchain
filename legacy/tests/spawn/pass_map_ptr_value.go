package main

type PtrItem struct {
	Value int32
}

type PtrContainer struct {
	Items map[string]*PtrItem
	First *PtrItem
}

func makePtrContainer() (c PtrContainer) {
	c.Items = map[string]*PtrItem{}
	item := &PtrItem{Value: 42}
	c.Items["a"] = item
	c.First = item
	return
}

func makePtrContainerPtr() (c *PtrContainer) {
	c = &PtrContainer{}
	c.Items = map[string]*PtrItem{}
	item := &PtrItem{Value: 99}
	c.Items["b"] = item
	c.First = item
	return
}

type NestedMapStruct struct {
	Inner NestedInner
	Tag   int32
}

type NestedInner struct {
	M map[string]int32
}

func makeNested() (n NestedMapStruct) {
	n = NestedMapStruct{
		Inner: NestedInner{M: map[string]int32{"n": 7, "m": 8}},
		Tag:   123,
	}
	return
}

func main() {
	c := makePtrContainer()
	if c.First.Value == 42 && c.Items["a"].Value == 42 {
		println("PASS:map-ptr-value-reloc")
	} else {
		println("FAIL:map-ptr-value-reloc")
	}

	cp := makePtrContainerPtr()
	if cp.First.Value == 99 && cp.Items["b"].Value == 99 {
		println("PASS:map-ptr-value-ptr-reloc")
	} else {
		println("FAIL:map-ptr-value-ptr-reloc")
	}

	n := makeNested()
	if n.Inner.M["n"] == 7 && n.Inner.M["m"] == 8 && n.Tag == 123 {
		println("PASS:map-nested-struct-return")
	} else {
		println("FAIL:map-nested-struct-return")
	}
}
