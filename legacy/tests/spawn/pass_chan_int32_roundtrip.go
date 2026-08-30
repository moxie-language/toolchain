package main

import "moxie"

// Phase B Test 1: builtin codec round-trip.
// Parent sends int32 values on send-channel; child receives, adds 1,
// sends back on a return channel; parent asserts each result.

func echoInc(in chan moxie.Int32, out chan moxie.Int32) {
	for i := int32(0); i < 4; i++ {
		v := <-in
		out <- moxie.Int32(int32(v) + 1)
	}
}

func main() {
	in := chan moxie.Int32{}
	out := chan moxie.Int32{}
	spawn(echoInc, in, out)

	for i := int32(0); i < 4; i++ {
		in <- moxie.Int32(i * 10)
		got := <-out
		want := i*10 + 1
		if int32(got) != want {
			println("FAIL:int32-roundtrip", i, int32(got), want)
			return
		}
	}
	println("PASS:int32-roundtrip")
}
