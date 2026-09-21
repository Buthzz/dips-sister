package rpc

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"
	"testing"

	"distapi/gen/cluster"
)

func createTestJPEG(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func TestWorkerServerProcessImage(t *testing.T) {
	ws := NewWorkerServer(nil)
	imgData := createTestJPEG(200, 100)

	req := &cluster.ProcessRequest{
		TaskId:       "task-test-01",
		JobId:        "job-test-01",
		ImageIndex:   0,
		ImageData:    imgData,
		Filename:     "sample.jpg",
		ResizeWidth:  100,
		ResizeHeight: 50,
		Grayscale:    true,
	}

	resp, err := ws.ProcessImage(context.Background(), req)
	if err != nil {
		t.Fatalf("ProcessImage error: %v", err)
	}

	if !resp.GetSuccess() {
		t.Fatalf("ProcessImage returned failure: %s", resp.GetError())
	}

	if len(resp.GetResultData()) == 0 {
		t.Fatal("hasil citra kosong")
	}

	outImg, _, err := image.Decode(bytes.NewReader(resp.GetResultData()))
	if err != nil {
		t.Fatalf("gagal decode hasil pemrosesan: %v", err)
	}

	bounds := outImg.Bounds()
	if bounds.Dx() > 100 || bounds.Dy() > 50 {
		t.Errorf("dimensi melebihi batas: %dx%d", bounds.Dx(), bounds.Dy())
	}
}

func TestWorkerServerProcessImageInvalid(t *testing.T) {
	ws := NewWorkerServer(nil)

	req := &cluster.ProcessRequest{
		TaskId:    "task-test-02",
		ImageData: []byte("bukan-citra-valid"),
		Filename:  "corrupt.jpg",
	}

	resp, err := ws.ProcessImage(context.Background(), req)
	if err != nil {
		t.Fatalf("ProcessImage error tidak terduga: %v", err)
	}

	if resp.GetSuccess() {
		t.Fatal("seharusnya mengembalikan success=false untuk data korup")
	}
}
