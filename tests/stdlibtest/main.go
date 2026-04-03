package main

import (
	"compress/bzip2"
	"compress/gzip"
	"compress/lzw"
	"compress/zlib"
	"container/list"
	"container/ring"
	"crypto/aes"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"hash/crc32"
	"hash/crc64"
	"math"
	mrand "math/rand/v2"
	"sort"
)

func main() {
	msg := []byte("moxie moxie moxie moxie moxie moxie")

	// Container/list
	{
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
	}

	// Container/ring
	{
		r := ring.New(3)
		for i := 0; i < 3; i++ {
			r.Value = i + 1
			r = r.Next()
		}
		sum := 0
		r.Do(func(v interface{}) {
			sum += v.(int)
		})
		print("ring sum: ")
		println(sum)
	}

	// Math
	{
		if math.Sqrt(2.0) > 1.4 && math.Sqrt(2.0) < 1.5 {
			println("math: ok")
		}
	}

	// Math/rand
	{
		print("rand: ")
		println(mrand.IntN(100))
	}

	// LZW
	{
		var buf byteBuffer
		w := lzw.NewWriter(&buf, lzw.LSB, 8)
		w.Write(msg)
		w.Close()
		r := lzw.NewReader(&byteReader{data: buf.data}, lzw.LSB, 8)
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
			println("lzw:  PASS")
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
		}
	}

	// Zlib
	{
		var buf byteBuffer
		w := zlib.NewWriter(&buf)
		w.Write(msg)
		w.Close()
		r, _ := zlib.NewReader(&byteReader{data: buf.data})
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
			println("zlib: PASS")
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
		block, _ := aes.NewCipher(key[:])
		print("aes:     block_size=")
		println(block.BlockSize())
	}

	// MD5
	{
		h := md5.Sum(msg)
		print("md5:     ")
		printHexN(h[:], 8)
		println()
	}

	// SHA1
	{
		h := sha1.Sum(msg)
		print("sha1:    ")
		printHexN(h[:], 8)
		println()
	}

	// Bzip2 (decompress only — bzip2 package is decompression only)
	{
		// A minimal bzip2 compressed stream for "hello"
		// Use pre-computed bzip2 data
		// bzip2 of "hello"
		compressed := []byte{
			0x42, 0x5a, 0x68, 0x39, 0x31, 0x41, 0x59, 0x26,
			0x53, 0x59, 0x19, 0x31, 0x65, 0x3d, 0x00, 0x00,
			0x00, 0x81, 0x00, 0x02, 0x44, 0xa0, 0x00, 0x21,
			0x9a, 0x68, 0x33, 0x4d, 0x07, 0x33, 0x8b, 0xb9,
			0x22, 0x9c, 0x28, 0x48, 0x0c, 0x98, 0xb2, 0x9e,
			0x80,
		}
		r := bzip2.NewReader(&byteReader{data: compressed})
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
		print("bzip2:   ")
		if len(result) > 0 {
			println("PASS")
		} else {
			println("FAIL")
		}
	}

	// Sort
	{
		data := []int{5, 3, 1, 4, 2}
		sort.Ints(data)
		sorted := true
		for i := 0; i < len(data)-1; i++ {
			if data[i] > data[i+1] {
				sorted = false
			}
		}
		print("sort:    ")
		if sorted && data[0] == 1 && data[4] == 5 {
			println("PASS")
		} else {
			println("FAIL")
		}
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

const hexchars = "0123456789abcdef"

func printHex(b byte) {
	print([]byte{hexchars[b>>4]})
	print([]byte{hexchars[b&0x0f]})
}

func printHexN(data []byte, n int) {
	for i := 0; i < n && i < len(data); i++ {
		printHex(data[i])
	}
	print("...")
}

func printHex32(v uint32) {
	for i := 28; i >= 0; i -= 4 {
		print([]byte{hexchars[byte((v>>uint32(i))&0x0f)]})
	}
}

func printHex64(v uint64) {
	for i := 60; i >= 0; i -= 4 {
		print([]byte{hexchars[byte((v>>uint64(i))&0x0f)]})
	}
}
