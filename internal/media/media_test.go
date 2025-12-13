package media

import (
	"testing"
)

func TestPathForVariant(t *testing.T) {
	m := NewManager("/root")
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
