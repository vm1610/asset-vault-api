package worker

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func makeTestJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatalf("encoding test jpeg: %v", err)
	}
	return buf.Bytes()
}

func makeTestPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encoding test png: %v", err)
	}
	return buf.Bytes()
}

func TestGenerateThumbnail_JPEGResizesLandscape(t *testing.T) {
	src := makeTestJPEG(t, 1024, 512)
	out, err := GenerateThumbnail(bytes.NewReader(src), "image/jpeg")
	if err != nil {
		t.Fatalf("generate thumbnail: %v", err)
	}

	img, err := jpeg.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("decoding generated thumbnail: %v", err)
	}
	bounds := img.Bounds()
	if bounds.Dx() != thumbnailMaxDimension {
		t.Fatalf("expected width %d, got %d", thumbnailMaxDimension, bounds.Dx())
	}
	if bounds.Dy() != 128 { // 512/1024 * 256
		t.Fatalf("expected height 128, got %d", bounds.Dy())
	}
}

func TestGenerateThumbnail_PNGResizesPortrait(t *testing.T) {
	src := makeTestPNG(t, 200, 800)
	out, err := GenerateThumbnail(bytes.NewReader(src), "image/png")
	if err != nil {
		t.Fatalf("generate thumbnail: %v", err)
	}

	img, err := jpeg.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("decoding generated thumbnail: %v", err)
	}
	bounds := img.Bounds()
	if bounds.Dy() != thumbnailMaxDimension {
		t.Fatalf("expected height %d, got %d", thumbnailMaxDimension, bounds.Dy())
	}
	if bounds.Dx() != 64 { // 200/800 * 256
		t.Fatalf("expected width 64, got %d", bounds.Dx())
	}
}

func TestGenerateThumbnail_SmallImageNotUpscaled(t *testing.T) {
	src := makeTestJPEG(t, 100, 80)
	out, err := GenerateThumbnail(bytes.NewReader(src), "image/jpeg")
	if err != nil {
		t.Fatalf("generate thumbnail: %v", err)
	}
	img, err := jpeg.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("decoding generated thumbnail: %v", err)
	}
	bounds := img.Bounds()
	if bounds.Dx() != 100 || bounds.Dy() != 80 {
		t.Fatalf("expected unchanged 100x80, got %dx%d", bounds.Dx(), bounds.Dy())
	}
}

func TestGenerateThumbnail_UnsupportedFormat(t *testing.T) {
	_, err := GenerateThumbnail(bytes.NewReader([]byte("not an image")), "video/mp4")
	if err != ErrUnsupportedFormat {
		t.Fatalf("expected ErrUnsupportedFormat, got %v", err)
	}
}
