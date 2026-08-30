package main

// Phase B Test 2: user-defined Codec round-trip.
// Point implements EncodeTo/DecodeFrom (8 bytes total). The native
// pipe routing uses raw memory layout (memcpy) rather than calling
// EncodeTo at the chan op site - both endpoints share the same
// architecture (fork inherits the binary). Codec compliance is still
// validated at compile time; the methods are part of the type's
// public API for non-fork transports.

import "io"

type Point struct {
	X int32
	Y int32
}

func (p *Point) EncodeTo(w io.Writer) (encErr error) {
	var b [8]byte
	b[0] = byte(p.X)
	b[1] = byte(p.X >> 8)
	b[2] = byte(p.X >> 16)
	b[3] = byte(p.X >> 24)
	b[4] = byte(p.Y)
	b[5] = byte(p.Y >> 8)
	b[6] = byte(p.Y >> 16)
	b[7] = byte(p.Y >> 24)
	_, encErr = w.Write(b[:])
	return encErr
}

func (p *Point) DecodeFrom(r io.Reader) (decErr error) {
	var b [8]byte
	_, decErr = io.ReadFull(r, b[:])
	p.X = int32(b[0]) | int32(b[1])<<8 | int32(b[2])<<16 | int32(b[3])<<24
	p.Y = int32(b[4]) | int32(b[5])<<8 | int32(b[6])<<16 | int32(b[7])<<24
	return decErr
}

func translate(in chan Point, out chan Point) {
	for i := int32(0); i < 3; i++ {
		p := <-in
		out <- Point{X: p.X + 100, Y: p.Y + 200}
	}
}

func main() {
	in := chan Point{}
	out := chan Point{}
	spawn(translate, in, out)

	for i := int32(0); i < 3; i++ {
		in <- Point{X: i, Y: i * 2}
		got := <-out
		if got.X != i+100 || got.Y != i*2+200 {
			println("FAIL:struct-roundtrip", i, got.X, got.Y)
			return
		}
	}
	println("PASS:struct-roundtrip")
}
