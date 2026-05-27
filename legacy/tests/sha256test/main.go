package main

import (
	"crypto/aes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"os"
)

func main() {
	// SHA-256
	h := sha256.New()
	h.Write([]byte("hello moxie"))
	sum := h.Sum(nil)
	os.Stdout.Write([]byte("sha256: "))
	printHex(sum)
	os.Stdout.Write([]byte("\n"))

	// SHA-512
	h2 := sha512.New()
	h2.Write([]byte("hello moxie"))
	sum2 := h2.Sum(nil)
	os.Stdout.Write([]byte("sha512: "))
	printHex(sum2)
	os.Stdout.Write([]byte("\n"))

	// HMAC-SHA256
	mac := hmac.New(sha256.New, []byte("secret key"))
	mac.Write([]byte("hello moxie"))
	macSum := mac.Sum(nil)
	os.Stdout.Write([]byte("hmac:   "))
	printHex(macSum)
	os.Stdout.Write([]byte("\n"))

	// crypto/rand
	random := []byte{:16:16}
	rand.Read(random)
	os.Stdout.Write([]byte("random: "))
	printHex(random)
	os.Stdout.Write([]byte("\n"))

	// AES
	key := []byte("0123456789abcdef") // 16 bytes for AES-128
	block, err := aes.NewCipher(key)
	if err != nil {
		os.Stdout.Write([]byte("aes error\n"))
		return
	}
	plain := []byte("attack at dawn!!")  // exactly 16 bytes
	cipher := []byte{:16:16}
	block.Encrypt(cipher, plain)
	os.Stdout.Write([]byte("aes:    "))
	printHex(cipher)
	os.Stdout.Write([]byte("\n"))
}

func printHex(data []byte) {
	hex := []byte("0123456789abcdef")
	for _, b := range data {
		os.Stdout.Write([]byte{hex[b>>4], hex[b&0x0f]})
	}
}
