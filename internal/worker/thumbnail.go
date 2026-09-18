// Package worker runs a pool of goroutines that pick up background jobs
// created by the API layer -- currently thumbnail generation for uploaded
// image assets -- and update job status as they progress.
package worker

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png" // register PNG decoder
	"io"

	"golang.org/x/image/draw"
)

const thumbnailMaxDimension = 256

// ErrUnsupportedFormat is returned when GenerateThumbnail is given a content
// type it does not know how to decode.
var ErrUnsupportedFormat = fmt.Errorf("worker: unsupported image format for thumbnailing")

// GenerateThumbnail decodes a JPEG or PNG image, resizes it so its longest
// edge is at most thumbnailMaxDimension pixels (preserving aspect ratio),
// and returns the result JPEG-encoded.
func GenerateThumbnail(src io.Reader, contentType string) ([]byte, error) {
	switch contentType {
	case "image/jpeg", "image/png":
	default:
		return nil, ErrUnsupportedFormat
	}

	img, _, err := image.Decode(src)
	if err != nil {
		return nil, fmt.Errorf("worker: decoding image: %w", err)
	}

	bounds := img.Bounds()
	srcW, srcH := bounds.Dx(), bounds.Dy()
	dstW, dstH := scaledDimensions(srcW, srcH, thumbnailMaxDimension)

	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, bounds, draw.Over, nil)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 85}); err != nil {
		return nil, fmt.Errorf("worker: encoding thumbnail: %w", err)
	}
	return buf.Bytes(), nil
}

func scaledDimensions(w, h, maxDim int) (int, int) {
	if w <= maxDim && h <= maxDim {
		return w, h
	}
	if w >= h {
		return maxDim, max(1, h*maxDim/w)
	}
	return max(1, w*maxDim/h), maxDim
}
