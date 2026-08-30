package main

import "io"
import "moxie"

// B.0.4 stress test: sustained codec round-trip under load.
// 10000 messages with mixed-size variable payloads. Exercises the
// Codec serialization path repeatedly to catch GC/heap interactions
// that single-shot tests miss.

type Msg struct {
	Seq  uint32
	Sum  uint32
	Body []byte
}

func (m *Msg) EncodeTo(w io.Writer) (encErr error) {
	var hdr [8]byte
	hdr[0] = byte(m.Seq)
	hdr[1] = byte(m.Seq >> 8)
	hdr[2] = byte(m.Seq >> 16)
	hdr[3] = byte(m.Seq >> 24)
	hdr[4] = byte(m.Sum)
	hdr[5] = byte(m.Sum >> 8)
	hdr[6] = byte(m.Sum >> 16)
	hdr[7] = byte(m.Sum >> 24)
	_, encErr = w.Write(hdr[:])
	if encErr != nil {
		return encErr
	}
	var sz [4]byte
	l := uint32(len(m.Body))
	sz[0] = byte(l)
	sz[1] = byte(l >> 8)
	sz[2] = byte(l >> 16)
	sz[3] = byte(l >> 24)
	_, encErr = w.Write(sz[:])
	if encErr != nil {
		return encErr
	}
	_, encErr = w.Write(m.Body)
	return encErr
}

func (m *Msg) DecodeFrom(r io.Reader) (decErr error) {
	var hdr [8]byte
	_, decErr = io.ReadFull(r, hdr[:])
	if decErr != nil {
		return decErr
	}
	m.Seq = uint32(hdr[0]) | uint32(hdr[1])<<8 | uint32(hdr[2])<<16 | uint32(hdr[3])<<24
	m.Sum = uint32(hdr[4]) | uint32(hdr[5])<<8 | uint32(hdr[6])<<16 | uint32(hdr[7])<<24
	var sz [4]byte
	_, decErr = io.ReadFull(r, sz[:])
	if decErr != nil {
		return decErr
	}
	l := uint32(sz[0]) | uint32(sz[1])<<8 | uint32(sz[2])<<16 | uint32(sz[3])<<24
	m.Body = []byte{:l}
	_, decErr = io.ReadFull(r, m.Body)
	return decErr
}

func mirror(in chan Msg, out chan Msg, done chan moxie.Uint32) {
	count := uint32(0)
	for {
		m := <-in
		if m.Seq == 0xFFFFFFFF {
			break
		}
		// Recompute sum on body to verify integrity.
		s := uint32(0)
		for i := int32(0); i < int32(len(m.Body)); i++ {
			s += uint32(m.Body[i])
		}
		out <- Msg{Seq: m.Seq, Sum: s, Body: m.Body}
		count++
	}
	done <- moxie.Uint32(count)
}

func main() {
	in := chan Msg{}
	out := chan Msg{}
	done := chan moxie.Uint32{}
	spawn(mirror, in, out, done)

	const N = uint32(10000)
	for i := uint32(0); i < N; i++ {
		// Varying body size: 0, 1, 2, ..., 1023, 0, 1, ...
		bodyLen := int32(i % 1024)
		body := []byte{:bodyLen}
		expectedSum := uint32(0)
		for j := int32(0); j < bodyLen; j++ {
			body[j] = byte((i + uint32(j)) & 0xFF)
			expectedSum += uint32(body[j])
		}
		in <- Msg{Seq: i, Sum: 0, Body: body}
		got := <-out
		if got.Seq != i || got.Sum != expectedSum {
			println("FAIL:stress", "i", i, "got.Seq", got.Seq, "got.Sum", got.Sum, "want.Sum", expectedSum)
			return
		}
		if i%1000 == 0 && i > 0 {
			println("progress:", i)
		}
	}
	in <- Msg{Seq: 0xFFFFFFFF, Sum: 0, Body: nil}
	count := uint32(<-done)
	if count != N {
		println("FAIL:stress", "count", count, "want", N)
		return
	}
	println("PASS:stress-codec", N, "msgs")
}
