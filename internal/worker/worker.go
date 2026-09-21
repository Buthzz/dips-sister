// Package worker menyediakan logika pemrosesan gambar yang murni dan stateless.
//
// Package ini sengaja tidak memiliki dependensi jaringan supaya bisa diuji
// secara mandiri tanpa koneksi ke master maupun node lain. Semua fungsi bersifat
// deterministik: input yang sama selalu menghasilkan output yang sama (idempoten),
// properti penting untuk semantik at-least-once execution di DESIGN.md §7.
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

// init mendaftarkan decoder JPEG dan PNG ke registry image.Decode.
// Tanpa baris ini, image.Decode tidak akan mengenali format tersebut
// karena Go menggunakan registrasi eksplisit untuk format gambar.
func init() {
	_ = jpeg.Decode
	_ = png.Decode
}

// Options menentukan transformasi apa yang diterapkan pada gambar.
// Nilai nol (zero value) berarti tidak ada transformasi — gambar hanya
// di-decode lalu di-encode ulang.
type Options struct {
	// ResizeWidth dan ResizeHeight menentukan dimensi maksimum hasil resize.
	// Jika hanya satu yang diset, dimensi lain dihitung otomatis agar
	// rasio aspek terjaga. Nilai 0 berarti dimensi itu tidak dibatasi.
	ResizeWidth  int
	ResizeHeight int

	// Grayscale mengubah gambar menjadi hitam-putih menggunakan formula luminansi.
	Grayscale bool
}

// Process adalah fungsi utama package ini. Ia menerima byte gambar mentah,
// menerapkan transformasi sesuai Options, lalu mengembalikan byte gambar hasil.
// Format output sama dengan format input (JPEG tetap JPEG, PNG tetap PNG).
//
// Error dikembalikan jika:
//   - imageData kosong
//   - format gambar tidak dikenali atau rusak
//   - encoding hasil gagal (sangat jarang, biasanya karena OOM)
func Process(imageData []byte, filename string, opts Options) ([]byte, error) {
	if len(imageData) == 0 {
		return nil, fmt.Errorf("worker: data gambar kosong")
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

// DetectFormat mendeteksi format gambar dari ekstensi nama file.
// Mengembalikan "jpeg" atau "png"; string kosong jika tidak dikenali.
// Fungsi ini hanya untuk labelling UI — proses decode tidak bergantung padanya.
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

// resize mengecilkan gambar agar muat di dalam kotak maxW × maxH
// dengan menjaga rasio aspek. Algoritma nearest-neighbour dipilih karena
// cepat dan cukup untuk demo; bisa diganti Lanczos jika kualitas perlu lebih baik.
//
// Jika hanya satu dimensi yang diset (>0), dimensi lain dihitung proporsional.
func resize(img image.Image, maxW, maxH int) image.Image {
	src := img.Bounds()
	srcW := src.Dx()
	srcH := src.Dy()

	if srcW == 0 || srcH == 0 {
		return img
	}

	dstW, dstH := maxW, maxH

	// Hitung dimensi yang belum diset berdasarkan rasio aspek.
	if dstW <= 0 {
		dstW = srcW * dstH / srcH
	}
	if dstH <= 0 {
		dstH = srcH * dstW / srcW
	}

	// Pilih skala yang lebih kecil agar gambar tidak keluar dari kotak.
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
			// Petakan piksel output ke piksel input menggunakan skala terbalik.
			srcX := x * srcW / dstW
			srcY := y * srcH / dstH
			out.Set(x, y, img.At(src.Min.X+srcX, src.Min.Y+srcY))
		}
	}
	return out
}

// toGrayscale mengonversi setiap piksel ke nilai abu-abu menggunakan
// model warna bawaan Go yang menerapkan formula luminansi standar ITU-R BT.601:
// Y = 0.299R + 0.587G + 0.114B
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
