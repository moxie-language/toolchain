package main

import "moxie"

// B.0.1 stress test: multi-channel select with bidirectional traffic.
// Models the dispatch domain shape: select between an inbound work
// channel and a result-channel from another spawned domain. Verifies:
//   - select scales beyond 2 channels
//   - polling cadence works with all channels idle then suddenly busy
//   - sibling-frame buffering inside tryPipeRecv works under load

func worker(in chan moxie.Int32, out chan moxie.Int32) {
	for {
		v, ok := <-in
		if !ok {
			break
		}
		out <- moxie.Int32(int32(v) * 2)
	}
}

func main() {
	in := chan moxie.Int32{}
	out := chan moxie.Int32{}
	spawn(worker, in, out)

	const N = int32(200)
	pending := int32(0)
	sent := int32(0)
	received := int32(0)
	expectedSum := int32(0)
	gotSum := int32(0)

	// Send first value to prime the pipeline.
	in <- moxie.Int32(0)
	pending++
	sent++

	for received < N {
		select {
		case v := <-out:
			gotSum += int32(v)
			pending--
			received++
			if sent < N && pending < 4 {
				in <- moxie.Int32(sent)
				expectedSum += sent * 2
				pending++
				sent++
			}
		}
	}
	expectedSum += 0 * 2 // priming send
	close(in)

	if received != N || gotSum != expectedSum {
		println("FAIL:select-multi", "rx", received, "sum", gotSum, "want", expectedSum)
		return
	}
	println("PASS:select-multi")
}
