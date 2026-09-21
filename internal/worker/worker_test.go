// Package worker — tes unit untuk logika pemrosesan gambar.
//
// Semua tes bersifat deterministic dan tidak membutuhkan jaringan atau disk.
// Gunakan pola table-driven agar mudah menambah kasus baru tanpa duplikasi kode.
package worker

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

// makeJPEG membuat gambar JPEG kosong berukuran w×h untuk keperluan tes.
func makeJPEG(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		panic("makeJPEG: " + err.Error())
	}
	return buf.Bytes()
}

// makePNG membuat gambar PNG kosong berukuran w×h untuk keperluan tes.
func makePNG(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic("makePNG: " + err.Error())
	}
	return buf.Bytes()
}

// TestProcessGrayscale memastikan grayscale menghasilkan gambar JPEG valid.
func TestProcessGrayscale(t *testing.T) {
	data := makeJPEG(100, 100)
	out, err := Process(data, "test.jpg", Options{Grayscale: true})
	if err != nil {
		t.Fatalf("Process grayscale: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("output kosong")
	}
}

// TestProcessResize memastikan dimensi output tidak melebihi batas yang diminta.
func TestProcessResize(t *testing.T) {
	data := makeJPEG(400, 200)
	out, err := Process(data, "test.jpg", Options{ResizeWidth: 100, ResizeHeight: 100})
	if err != nil {
		t.Fatalf("Process resize: %v", err)
	}

	img, _, err := image.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("decode hasil resize: %v", err)
	}

	b := img.Bounds()
	if b.Dx() > 100 || b.Dy() > 100 {
		t.Errorf("dimensi melebihi batas: %dx%d", b.Dx(), b.Dy())
	}
}

// TestProcessResizeAspectRatio memastikan rasio aspek dipertahankan saat resize.
func TestProcessResizeAspectRatio(t *testing.T) {
	// Gambar 400×200 (2:1) — resize ke max 200×200 harus menghasilkan ~200×100.
	data := makeJPEG(400, 200)
	out, err := Process(data, "test.jpg", Options{ResizeWidth: 200, ResizeHeight: 200})
	if err != nil {
		t.Fatalf("Process resize aspek: %v", err)
	}

	img, _, err := image.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	b := img.Bounds()
	// Toleransi ±2 piksel karena pembulatan integer saat scaling.
	if b.Dx() > 202 || b.Dy() > 102 {
		t.Errorf("rasio aspek berubah: hasil %dx%d, harusnya sekitar 200x100", b.Dx(), b.Dy())
	}
}

// TestProcessPNG memastikan PNG juga bisa diproses dengan benar.
func TestProcessPNG(t *testing.T) {
	data := makePNG(50, 50)
	out, err := Process(data, "logo.png", Options{Grayscale: true, ResizeWidth: 25, ResizeHeight: 25})
	if err != nil {
		t.Fatalf("Process PNG: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("output PNG kosong")
	}
}

// TestProcessNoOp memastikan tanpa transformasi, gambar tetap bisa di-roundtrip.
func TestProcessNoOp(t *testing.T) {
	data := makeJPEG(50, 50)
	out, err := Process(data, "test.jpg", Options{})
	if err != nil {
		t.Fatalf("Process no-op: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("output kosong")
	}
}

// TestProcessEmptyData memastikan input kosong menghasilkan error, bukan panic.
func TestProcessEmptyData(t *testing.T) {
	_, err := Process(nil, "test.jpg", Options{})
	if err == nil {
		t.Fatal("harusnya error untuk input kosong")
	}
}

// TestProcessInvalidData memastikan data biner acak menghasilkan error decode.
func TestProcessInvalidData(t *testing.T) {
	_, err := Process([]byte("ini bukan gambar"), "test.jpg", Options{})
	if err == nil {
		t.Fatal("harusnya error untuk data tidak valid")
	}
}

// TestDetectFormat memverifikasi deteksi format dari ekstensi nama file.
func TestDetectFormat(t *testing.T) {
	kasus := []struct {
		namaFile string
		format   string
	}{
		{"foto.jpg", "jpeg"},
		{"foto.jpeg", "jpeg"},
		{"FOTO.JPG", "jpeg"},  // harus case-insensitive
		{"logo.png", "png"},
		{"LOGO.PNG", "png"},
		{"video.mp4", ""},     // format tidak didukung
		{"tanpa-ekstensi", ""},
	}

	for _, k := range kasus {
		t.Run(k.namaFile, func(t *testing.T) {
			got := DetectFormat(k.namaFile)
			if got != k.format {
				t.Errorf("DetectFormat(%q): mau %q, dapat %q", k.namaFile, k.format, got)
			}
		})
	}
}

// TestToGrayscaleOutput memastikan pixel gambar grayscale memiliki nilai R=G=B.
func TestToGrayscaleOutput(t *testing.T) {
	// Buat gambar berwarna merah murni.
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := range 4 {
		for x := range 4 {
			img.Set(x, y, color.RGBA{R: 255, G: 0, B: 0, A: 255})
		}
	}

	gray := toGrayscale(img)

	// Encode dan decode ulang untuk memverifikasi via interface publik.
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, gray, nil)

	result, _, err := image.Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("decode grayscale result: %v", err)
	}

	// Gambar grayscale memiliki R ≈ G ≈ B di setiap piksel.
	r, g, b, _ := result.At(2, 2).RGBA()
	// Toleransi 500 karena JPEG lossy compression.
	diff := func(a, b uint32) uint32 {
		if a > b {
			return a - b
		}
		return b - a
	}
	if diff(r, g) > 500 || diff(r, b) > 500 {
		t.Errorf("piksel tidak grayscale: R=%d G=%d B=%d", r>>8, g>>8, b>>8)
	}
}

// TestResizeSingleDimension memastikan resize dengan satu dimensi saja tidak panic.
func TestResizeSingleDimension(t *testing.T) {
	data := makeJPEG(200, 100)

	// Hanya lebar yang diset — tinggi harus dihitung otomatis.
	out, err := Process(data, "test.jpg", Options{ResizeWidth: 100})
	if err != nil {
		t.Fatalf("resize width only: %v", err)
	}
	img, _, _ := image.Decode(bytes.NewReader(out))
	if img.Bounds().Dx() > 100 {
		t.Errorf("lebar melebihi batas: %d", img.Bounds().Dx())
	}
}
