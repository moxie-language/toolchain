package main

import "io"

// B.0.4 test: chan element type with []byte slice field.
// Before Codec invocation, raw memcpy across the spawn boundary sent
// only the slice header (ptr/len/cap), causing the receiver to
// dereference a parent-only address - SIGSEGV.
//
// With Codec, EncodeTo writes the byte payload as length-prefixed
// data; DecodeFrom allocates a fresh slice in the receiver's heap.

type Blob struct {
	Size int32
	Data []byte
}

func (b *Blob) EncodeTo(w io.Writer) (encErr error) {
	var sz [4]byte
	sz[0] = byte(b.Size)
	sz[1] = byte(b.Size >> 8)
	sz[2] = byte(b.Size >> 16)
	sz[3] = byte(b.Size >> 24)
	_, encErr = w.Write(sz[:])
	if encErr != nil {
		return encErr
	}
	_, encErr = w.Write(b.Data)
	return encErr
}

func (b *Blob) DecodeFrom(r io.Reader) (decErr error) {
	var sz [4]byte
	_, decErr = io.ReadFull(r, sz[:])
	if decErr != nil {
		return decErr
	}
	b.Size = int32(sz[0]) | int32(sz[1])<<8 | int32(sz[2])<<16 | int32(sz[3])<<24
	b.Data = make([]byte, b.Size)
	_, decErr = io.ReadFull(r, b.Data)
	return decErr
}

func echo(in chan Blob, out chan Blob) {
	b := <-in
	bad := int32(-1)
	for i := int32(0); i < b.Size; i++ {
		if b.Data[i] != byte(i) {
			bad = i
			break
		}
	}
	if bad != -1 {
		out <- Blob{Size: bad, Data: nil}
		return
	}
	out <- Blob{Size: b.Size, Data: b.Data}
}

func main() {
	const N = int32(100000) // 100 KB - exceeds kernel pipe buffer to exercise partial syscalls
	in := make(chan Blob)
	out := make(chan Blob)
	spawn(echo, in, out)

	data := make([]byte, N)
	for i := int32(0); i < N; i++ {
		data[i] = byte(i)
	}
	in <- Blob{Size: N, Data: data}

	got := <-out
	if got.Size != N {
		println("FAIL:slice", "size", got.Size, "want", N)
		return
	}
	for i := int32(0); i < N; i++ {
		if got.Data[i] != byte(i) {
			println("FAIL:slice", "byte mismatch at", i)
			return
		}
	}
	println("PASS:slice-codec")
}
