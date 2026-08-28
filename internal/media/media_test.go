package media

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"testing"
)

func TestPathForVariant(t *testing.T) {
	m := NewManager("/root", VariantConfig{ContentMaxWidth: 1600, ThumbMaxWidth: 400})
	sha := "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	orig := m.PathForVariant(sha, VariantOriginal, ".jpg")
	if orig != "/root/original/ab/cd/"+sha+".jpg" {
		t.Fatalf("unexpected original path: %s", orig)
	}
	content := m.PathForVariant(sha, VariantContent, ".jpg")
	if content != "/root/content/ab/cd/"+sha+".webp" {
		t.Fatalf("unexpected content path: %s", content)
	}
	thumb := m.PathForVariant(sha, VariantThumb, ".jpg")
	if thumb != "/root/thumb/ab/cd/"+sha+".webp" {
		t.Fatalf("unexpected thumb path: %s", thumb)
	}
}

func TestSaveGeneratesBoundedWebPVariantsAndPreservesOriginal(t *testing.T) {
	source := encodePNG(t, 200, 100)
	manager := NewManager(t.TempDir(), VariantConfig{ContentMaxWidth: 80, ThumbMaxWidth: 30})

	result, err := manager.Save(context.Background(), bytes.NewReader(source), "source.png", int64(len(source)), 20_000)
	if err != nil {
		t.Fatalf("save image: %v", err)
	}

	original, err := os.ReadFile(manager.PathForVariant(result.SHA256, VariantOriginal, result.Ext))
	if err != nil {
		t.Fatalf("read original: %v", err)
	}
	if !bytes.Equal(original, source) {
		t.Fatal("stored original differs from uploaded bytes")
	}

	assertWebPDimensions(t, manager.PathForVariant(result.SHA256, VariantContent, result.Ext), 80, 40)
	assertWebPDimensions(t, manager.PathForVariant(result.SHA256, VariantThumb, result.Ext), 30, 15)
}

func TestSaveDoesNotUpscaleVariants(t *testing.T) {
	source := encodePNG(t, 40, 20)
	manager := NewManager(t.TempDir(), VariantConfig{ContentMaxWidth: 80, ThumbMaxWidth: 60})

	result, err := manager.Save(context.Background(), bytes.NewReader(source), "source.png", int64(len(source)), 800)
	if err != nil {
		t.Fatalf("save image: %v", err)
	}

	assertWebPDimensions(t, manager.PathForVariant(result.SHA256, VariantContent, result.Ext), 40, 20)
	assertWebPDimensions(t, manager.PathForVariant(result.SHA256, VariantThumb, result.Ext), 40, 20)
}

func encodePNG(t *testing.T, width, height int) []byte {
	t.Helper()
	source := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			source.Set(x, y, color.RGBA{
				R: uint8(x % 256),
				G: uint8(y % 256),
				B: uint8((x + y) % 256),
				A: 255,
			})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, source); err != nil {
		t.Fatalf("encode source PNG: %v", err)
	}
	return encoded.Bytes()
}

func assertWebPDimensions(t *testing.T, path string, width, height int) {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open variant: %v", err)
	}
	defer file.Close()

	decoded, format, err := image.Decode(file)
	if err != nil {
		t.Fatalf("decode variant: %v", err)
	}
	if format != "webp" {
		t.Fatalf("variant format = %q, want webp", format)
	}
	bounds := decoded.Bounds()
	if bounds.Dx() != width || bounds.Dy() != height {
		t.Fatalf("variant dimensions = %dx%d, want %dx%d", bounds.Dx(), bounds.Dy(), width, height)
	}
}
