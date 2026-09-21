// Package worker menyediakan fungsi pemrosesan citra digital secara stateless.
package worker

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
)

func init() {
	_ = jpeg.Decode
	_ = png.Decode
}

// Options menentukan parameter transformasi citra.
type Options struct {
	ResizeWidth  int
	ResizeHeight int
	Grayscale    bool
}

// Process memproses data citra biner sesuai opsi dan mengembalikan hasilnya.
func Process(imageData []byte, filename string, opts Options) ([]byte, error) {
	if len(imageData) == 0 {
		return nil, fmt.Errorf("worker: data citra kosong")
	}

	img, format, err := image.Decode(bytes.NewReader(imageData))
	if err != nil {
		return nil, fmt.Errorf("worker: gagal decode %s: %w", filename, err)
	}

	if opts.ResizeWidth > 0 || opts.ResizeHeight > 0 {
		img = resize(img, opts.ResizeWidth, opts.ResizeHeight)
	}

	if opts.Grayscale {
		img = toGrayscale(img)
	}

	var buf bytes.Buffer
	switch format {
	case "jpeg":
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
			return nil, fmt.Errorf("worker: gagal encode jpeg: %w", err)
		}
	case "png":
		if err := png.Encode(&buf, img); err != nil {
			return nil, fmt.Errorf("worker: gagal encode png: %w", err)
		}
	default:
		return nil, fmt.Errorf("worker: format tidak didukung %q", format)
	}

	return buf.Bytes(), nil
}

// DetectFormat mengenali format citra berdasarkan ekstensi berkas.
func DetectFormat(filename string) string {
	lower := strings.ToLower(filename)
	switch {
	case strings.HasSuffix(lower, ".jpg"), strings.HasSuffix(lower, ".jpeg"):
		return "jpeg"
	case strings.HasSuffix(lower, ".png"):
		return "png"
	}
	return ""
}

// resize mengubah dimensi citra dengan mempertahankan rasio aspek asli (nearest-neighbour).
func resize(img image.Image, maxW, maxH int) image.Image {
	src := img.Bounds()
	srcW := src.Dx()
	srcH := src.Dy()

	if srcW == 0 || srcH == 0 {
		return img
	}

	dstW, dstH := maxW, maxH

	if dstW <= 0 {
		dstW = srcW * dstH / srcH
	}
	if dstH <= 0 {
		dstH = srcH * dstW / srcW
	}

	scaleW := float64(dstW) / float64(srcW)
	scaleH := float64(dstH) / float64(srcH)
	scale := scaleW
	if scaleH < scaleW {
		scale = scaleH
	}

	dstW = max(1, int(float64(srcW)*scale))
	dstH = max(1, int(float64(srcH)*scale))

	out := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	for y := range dstH {
		for x := range dstW {
			srcX := x * srcW / dstW
			srcY := y * srcH / dstH
			out.Set(x, y, img.At(src.Min.X+srcX, src.Min.Y+srcY))
		}
	}
	return out
}

// toGrayscale mengonversi piksel citra ke grayscale menggunakan model ITU-R BT.601.
func toGrayscale(img image.Image) image.Image {
	bounds := img.Bounds()
	out := image.NewGray(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			out.Set(x, y, color.GrayModel.Convert(img.At(x, y)))
		}
	}
	return out
}
