package main

import (
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"path/filepath"
)

// Program utilitas untuk menghasilkan berkas citra pengujian (testdata).
// Berkas ini digunakan untuk pengujian fungsional, performa, dan demonstrasi sistem.
func main() {
	targetDir := "testdata"
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		fmt.Printf("Gagal membuat direktori %s: %v\n", targetDir, err)
		os.Exit(1)
	}

	buatGradientJPEG(filepath.Join(targetDir, "sample_1080p.jpg"), 1920, 1080)
	buatGeometricJPEG(filepath.Join(targetDir, "sample_square.jpg"), 800, 800)
	buatLandscapeJPEG(filepath.Join(targetDir, "sample_landscape.jpg"), 1200, 600)
	buatColorBarsPNG(filepath.Join(targetDir, "sample_portrait.png"), 600, 1200)
	buatCirclePNG(filepath.Join(targetDir, "sample_small.png"), 200, 200)
	buatGradientJPEG(filepath.Join(targetDir, "sample_large_2k.jpg"), 2560, 1440)

	fmt.Println("Semua berkas citra uji berhasil dibuat di folder testdata/")
}

// buatGradientJPEG menghasilkan citra JPEG dengan spektrum warna gradien halus.
func buatGradientJPEG(path string, w, h int) {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r := uint8((float64(x) / float64(w)) * 255)
			g := uint8((float64(y) / float64(h)) * 255)
			b := uint8((float64(x+y) / float64(w+h)) * 255)
			img.Set(x, y, color.RGBA{R: r, G: g, B: b, A: 255})
		}
	}
	simpanJPEG(path, img, 90)
}

// buatGeometricJPEG menghasilkan citra dengan pola kisi-kisi dan kotak catur.
func buatGeometricJPEG(path string, w, h int) {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	ukuranKotak := 50
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			pola := (x/ukuranKotak + y/ukuranKotak) % 2
			if pola == 0 {
				img.Set(x, y, color.RGBA{R: 220, G: 50, B: 50, A: 255})
			} else {
				img.Set(x, y, color.RGBA{R: 50, G: 120, B: 220, A: 255})
			}
		}
	}
	simpanJPEG(path, img, 85)
}

// buatLandscapeJPEG menghasilkan pemandangan geometris dengan langit dan bukit.
func buatLandscapeJPEG(path string, w, h int) {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	tengahY := h / 2
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if y < tengahY {
				langitB := uint8(200 + (float64(y)/float64(tengahY))*55)
				img.Set(x, y, color.RGBA{R: 100, G: 160, B: langitB, A: 255})
			} else {
				gelombang := int(float64(h)/10.0 * math.Sin(float64(x)/50.0))
				if y > tengahY+gelombang {
					img.Set(x, y, color.RGBA{R: 40, G: 160, B: 60, A: 255})
				} else {
					img.Set(x, y, color.RGBA{R: 70, G: 190, B: 90, A: 255})
				}
			}
		}
	}
	simpanJPEG(path, img, 90)
}

// buatColorBarsPNG menghasilkan pola batang warna televisi standar.
func buatColorBarsPNG(path string, w, h int) {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	warnaBatang := []color.RGBA{
		{R: 255, G: 255, B: 255, A: 255}, // Putih
		{R: 255, G: 255, B: 0, A: 255},   // Kuning
		{R: 0, G: 255, B: 255, A: 255},   // Cyan
		{R: 0, G: 255, B: 0, A: 255},     // Hijau
		{R: 255, G: 0, B: 255, A: 255},   // Magenta
		{R: 255, G: 0, B: 0, A: 255},     // Merah
		{R: 0, G: 0, B: 255, A: 255},     // Biru
	}

	lebarBatang := w / len(warnaBatang)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			idx := x / lebarBatang
			if idx >= len(warnaBatang) {
				idx = len(warnaBatang) - 1
			}
			img.Set(x, y, warnaBatang[idx])
		}
	}
	simpanPNG(path, img)
}

// buatCirclePNG menghasilkan lingkaran di atas latar belakang transparan.
func buatCirclePNG(path string, w, h int) {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	radius := float64(min(w, h)) * 0.4
	tengahX, tengahY := float64(w)/2, float64(h)/2

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			jarak := math.Hypot(float64(x)-tengahX, float64(y)-tengahY)
			if jarak <= radius {
				img.Set(x, y, color.RGBA{R: 255, G: 140, B: 0, A: 255})
			} else {
				img.Set(x, y, color.RGBA{R: 240, G: 240, B: 240, A: 255})
			}
		}
	}
	simpanPNG(path, img)
}

func simpanJPEG(path string, img image.Image, quality int) {
	f, err := os.Create(path)
	if err != nil {
		fmt.Printf("Gagal membuat %s: %v\n", path, err)
		return
	}
	defer f.Close()

	if err := jpeg.Encode(f, img, &jpeg.Options{Quality: quality}); err != nil {
		fmt.Printf("Gagal encode %s: %v\n", path, err)
		return
	}
	info, _ := f.Stat()
	fmt.Printf("[JPEG] %s (%d bytes)\n", path, info.Size())
}

func simpanPNG(path string, img image.Image) {
	f, err := os.Create(path)
	if err != nil {
		fmt.Printf("Gagal membuat %s: %v\n", path, err)
		return
	}
	defer f.Close()

	if err := png.Encode(f, img); err != nil {
		fmt.Printf("Gagal encode %s: %v\n", path, err)
		return
	}
	info, _ := f.Stat()
	fmt.Printf("[PNG]  %s (%d bytes)\n", path, info.Size())
}
