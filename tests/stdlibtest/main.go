package main

import (
	"compress/gzip"
	"compress/lzw"
	"container/list"
	"crypto/aes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"hash/crc32"
	"hash/crc64"
)

func main() {
	msg := []byte("moxie moxie moxie moxie moxie moxie")

	// Container/list
	l := list.New()
	l.PushBack(42)
	l.PushBack(99)
	l.PushBack(7)
	print("list: ")
	for e := l.Front(); e != nil; e = e.Next() {
		print(e.Value.(int))
		print(" ")
	}
	println()

	// LZW
	{
		var buf byteBuffer
		w := lzw.NewWriter(&buf, lzw.LSB, 8)
		w.Write(msg)
		w.Close()
		rd := &byteReader{data: buf.data}
		r := lzw.NewReader(rd, lzw.LSB, 8)
		var result []byte
		var tmp [256]byte
		for {
			n, err := r.Read(tmp[:])
			if n > 0 {
				result = append(result, tmp[:n]...)
			}
			if err != nil {
				break
			}
		}
		r.Close()
		if bytesEqual(result, msg) {
			println("lzw: PASS")
		} else {
			println("lzw: FAIL")
		}
	}

	// Gzip
	{
		var buf byteBuffer
		w := gzip.NewWriter(&buf)
		w.Write(msg)
		w.Close()
		r, _ := gzip.NewReader(&byteReader{data: buf.data})
		var result []byte
		var tmp [256]byte
		for {
			n, err := r.Read(tmp[:])
			if n > 0 {
				result = append(result, tmp[:n]...)
			}
			if err != nil {
				break
			}
		}
		r.Close()
		if bytesEqual(result, msg) {
			println("gzip: PASS")
		} else {
			println("gzip: FAIL")
		}
	}

	// Crypto
	{
		h := sha256.Sum256(msg)
		print("sha256:  ")
		printHexN(h[:], 8)
		println()
	}
	{
		h := sha512.Sum512(msg)
		print("sha512:  ")
		printHexN(h[:], 8)
		println()
	}
	{
		mac := hmac.New(sha256.New, []byte("secret"))
		mac.Write(msg)
		h := mac.Sum(nil)
		print("hmac:    ")
		printHexN(h, 8)
		println()
	}
	{
		print("crc32:   ")
		printHex32(crc32.ChecksumIEEE(msg))
		println()
	}
	{
		tab := crc64.MakeTable(crc64.ECMA)
		print("crc64:   ")
		printHex64(crc64.Checksum(msg, tab))
		println()
	}
	{
		var key [16]byte
		rand.Read(key[:])
		block, err := aes.NewCipher(key[:])
		if err != nil {
			println("aes: error")
		} else {
			print("aes:     block_size=")
			println(block.BlockSize())
		}
	}
	{
		var buf [8]byte
		rand.Read(buf[:])
		print("random:  ")
		for _, b := range buf {
			printHex(b)
		}
		println()
	}
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

type byteBuffer struct{ data []byte }

func (b *byteBuffer) Write(p []byte) (int, error) {
	b.data = append(b.data, p...)
	return len(p), nil
}

type byteReader struct {
	data []byte
	pos  int
}

func (r *byteReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, eof{}
	}
	n := copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

type eof struct{}

func (eof) Error() string { return "EOF" }

const hex = "0123456789abcdef"

func printHex(b byte) {
	print([]byte{hex[b>>4]})
	print([]byte{hex[b&0x0f]})
}

func printHexN(data []byte, n int) {
	for i := 0; i < n && i < len(data); i++ {
		printHex(data[i])
	}
	print("...")
}

func printHex32(v uint32) {
	for i := 28; i >= 0; i -= 4 {
		print([]byte{hex[byte((v>>uint32(i))&0x0f)]})
	}
}

func printHex64(v uint64) {
	for i := 60; i >= 0; i -= 4 {
		print([]byte{hex[byte((v>>uint64(i))&0x0f)]})
	}
}
