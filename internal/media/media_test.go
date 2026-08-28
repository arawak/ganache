package media

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
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
func TestFailedDuplicateUploadPreservesExistingFiles(t *testing.T) {
	source := encodePNG(t, 200, 100)
	root := t.TempDir()
	manager := NewManager(root, VariantConfig{ContentMaxWidth: 80, ThumbMaxWidth: 30})
	result, err := manager.Save(
		context.Background(),
		bytes.NewReader(source),
		"source.png",
		int64(len(source)),
		20_000,
	)
	if err != nil {
		t.Fatalf("save initial image: %v", err)
	}

	paths := []string{
		manager.PathForVariant(result.SHA256, VariantOriginal, result.Ext),
		manager.PathForVariant(result.SHA256, VariantContent, result.Ext),
		manager.PathForVariant(result.SHA256, VariantThumb, result.Ext),
	}
	before := make([][]byte, len(paths))
	for index, path := range paths {
		before[index], err = os.ReadFile(path)
		if err != nil {
			t.Fatalf("read initial file %q: %v", path, err)
		}
	}

	invalidManager := NewManager(root, VariantConfig{ContentMaxWidth: 80, ThumbMaxWidth: 0})
	if _, err := invalidManager.Save(
		context.Background(),
		bytes.NewReader(source),
		"source.png",
		int64(len(source)),
		20_000,
	); err == nil {
		t.Fatal("duplicate upload with an invalid thumbnail width succeeded")
	}

	for index, path := range paths {
		after, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read file after failed duplicate upload %q: %v", path, err)
		}
		if !bytes.Equal(after, before[index]) {
			t.Fatalf("file %q changed after failed duplicate upload", path)
		}
	}
}

func TestSaveAppliesEXIFOrientationToGeneratedVariants(t *testing.T) {
	source := encodeOrientedJPEG(t, 2, 1, 6)
	manager := NewManager(t.TempDir(), VariantConfig{ContentMaxWidth: 80, ThumbMaxWidth: 60})

	result, err := manager.Save(
		context.Background(),
		bytes.NewReader(source),
		"portrait.jpg",
		int64(len(source)),
		2,
	)
	if err != nil {
		t.Fatalf("save oriented JPEG: %v", err)
	}
	if result.Width != 1 || result.Height != 2 {
		t.Fatalf("asset dimensions = %dx%d, want display-oriented 1x2", result.Width, result.Height)
	}

	original, err := os.ReadFile(manager.PathForVariant(result.SHA256, VariantOriginal, result.Ext))
	if err != nil {
		t.Fatalf("read oriented original: %v", err)
	}
	if !bytes.Equal(original, source) {
		t.Fatal("stored oriented original differs from uploaded bytes")
	}

	assertWebPDimensions(t, manager.PathForVariant(result.SHA256, VariantContent, result.Ext), 1, 2)
	assertWebPDimensions(t, manager.PathForVariant(result.SHA256, VariantThumb, result.Ext), 1, 2)
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

func encodeOrientedJPEG(t *testing.T, width, height int, orientation byte) []byte {
	t.Helper()
	source := image.NewRGBA(image.Rect(0, 0, width, height))
	source.Set(0, 0, color.RGBA{R: 255, A: 255})
	if width > 1 {
		source.Set(1, 0, color.RGBA{B: 255, A: 255})
	}

	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, source, &jpeg.Options{Quality: 100}); err != nil {
		t.Fatalf("encode source JPEG: %v", err)
	}

	exifPayload := []byte{
		'E', 'x', 'i', 'f', 0, 0,
		'I', 'I', 0x2a, 0,
		8, 0, 0, 0,
		1, 0,
		0x12, 0x01,
		3, 0,
		1, 0, 0, 0,
		orientation, 0, 0, 0,
		0, 0, 0, 0,
	}
	segmentLength := len(exifPayload) + 2
	exifSegment := []byte{0xff, 0xe1, byte(segmentLength >> 8), byte(segmentLength)}
	exifSegment = append(exifSegment, exifPayload...)

	jpegBytes := encoded.Bytes()
	oriented := make([]byte, 0, len(jpegBytes)+len(exifSegment))
	oriented = append(oriented, jpegBytes[:2]...)
	oriented = append(oriented, exifSegment...)
	oriented = append(oriented, jpegBytes[2:]...)
	return oriented
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
