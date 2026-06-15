package vision

import (
	"bytes"
	"image"
	"image/jpeg"
	"testing"
)

func makeJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return buf.Bytes()
}

func TestAnnotateJPEGProducesValidImageOfSameSize(t *testing.T) {
	src := makeJPEG(t, 640, 360)
	out := AnnotateJPEG(src, []AnnotatedBox{{Box: Box{X: 0.5, Y: 0.3, W: 0.2, H: 0.4}, Label: "Person"}}, 85)

	if len(out) == 0 {
		t.Fatal("annotated output is empty")
	}
	if bytes.Equal(out, src) {
		t.Error("expected annotated bytes to differ from source")
	}
	img, err := jpeg.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("annotated output is not valid jpeg: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 640 || b.Dy() != 360 {
		t.Errorf("dimensions changed: %dx%d", b.Dx(), b.Dy())
	}
}

func TestAnnotateJPEGNoBoxesReturnsOriginal(t *testing.T) {
	src := makeJPEG(t, 64, 64)
	if out := AnnotateJPEG(src, nil, 85); !bytes.Equal(out, src) {
		t.Error("no boxes should return the original bytes unchanged")
	}
}

func TestAnnotateJPEGInvalidInputReturnsOriginal(t *testing.T) {
	junk := []byte("not a jpeg")
	out := AnnotateJPEG(junk, []AnnotatedBox{{Box: Box{X: 0.1, Y: 0.1, W: 0.2, H: 0.2}, Label: "x"}}, 85)
	if !bytes.Equal(out, junk) {
		t.Error("undecodable input should be returned unchanged")
	}
}
