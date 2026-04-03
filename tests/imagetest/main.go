package main

import (
	"image"
	"image/color"
	"image/png"
	"os"
)

func main() {
	// Create a 64x64 gradient image
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			img.Set(x, y, color.RGBA{
				R: uint8(x * 4),
				G: uint8(y * 4),
				B: uint8((x + y) * 2),
				A: 255,
			})
		}
	}

	// Write to file
	f, err := os.Create("/tmp/moxie_test.png")
	if err != nil {
		print("create error: ")
		println(err.Error())
		return
	}

	err = png.Encode(f, img)
	f.Close()
	if err != nil {
		print("encode error: ")
		println(err.Error())
		return
	}

	// Read back
	f2, err := os.Open("/tmp/moxie_test.png")
	if err != nil {
		print("open error: ")
		println(err.Error())
		return
	}
	decoded, err := png.Decode(f2)
	f2.Close()
	if err != nil {
		print("decode error: ")
		println(err.Error())
		return
	}

	// Verify dimensions
	b := decoded.Bounds()
	print("decoded: ")
	print(b.Dx())
	print("x")
	println(b.Dy())

	// Verify a pixel
	r, g, bl, a := decoded.At(32, 32).RGBA()
	print("pixel(32,32): R=")
	print(r >> 8)
	print(" G=")
	print(g >> 8)
	print(" B=")
	print(bl >> 8)
	print(" A=")
	println(a >> 8)

	println("PNG encode/decode: PASS")
}
