package main

import "moxie"

// Phase B Test 3: close propagation.
// Parent sends N values then closes the send-channel. Child recv loop
// drains values; the comma-ok recv returns ok=false once the parent's
// close marker traverses the pipe. Child writes a final completion
// value to the parent over a return channel so the parent can verify
// the count and close behavior.

func drain(in chan moxie.Int32, done chan moxie.Int32) {
	count := int32(0)
	sum := int32(0)
	for {
		v, ok := <-in
		if !ok {
			break
		}
		count++
		sum += int32(v)
	}
	// Encode count in low 16 bits, sum in high 16 bits — single
	// completion value over the return channel.
	done <- moxie.Int32(count | (sum << 16))
}

func main() {
	in := chan moxie.Int32{}
	done := chan moxie.Int32{}
	spawn(drain, in, done)

	const N = int32(5)
	expectedSum := int32(0)
	for i := int32(1); i <= N; i++ {
		in <- moxie.Int32(i)
		expectedSum += i
	}
	close(in)

	result := int32(<-done)
	gotCount := result & 0xFFFF
	gotSum := (result >> 16) & 0xFFFF
	if gotCount != N || gotSum != expectedSum {
		println("FAIL:close-propagation", gotCount, gotSum, "expected", N, expectedSum)
		return
	}
	println("PASS:close-propagation")
}
