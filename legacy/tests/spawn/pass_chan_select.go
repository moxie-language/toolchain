package main

import "moxie"

// B.0.1 test: select on pipe-bound channels.
// Parent uses select to multiplex between two pipe-bound channels
// (one receives results, one is the timeout/control signal). Verifies:
//   - tryRecv path on pipe-bound channels works
//   - blocking poll loop wakes when pipe data arrives
//   - mixed-readiness case (one channel ready, one not)

func producer(out chan moxie.Int32, ctrl chan moxie.Int32) {
	for i := int32(0); i < 5; i++ {
		out <- moxie.Int32(i)
	}
	ctrl <- moxie.Int32(99)
}

func main() {
	out := chan moxie.Int32{}
	ctrl := chan moxie.Int32{}
	spawn(producer, out, ctrl)

	count := int32(0)
	sum := int32(0)
	done := false
	for !done {
		select {
		case v := <-out:
			count++
			sum += int32(v)
		case c := <-ctrl:
			if int32(c) != 99 {
				println("FAIL:select", "wrong ctrl value", int32(c))
				return
			}
			done = true
		}
	}

	if count != 5 || sum != 0+1+2+3+4 {
		println("FAIL:select", "count", count, "sum", sum)
		return
	}
	println("PASS:select-pipe-bound")
}
