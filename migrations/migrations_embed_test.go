package migrations

import (
	"io/fs"
	"testing"
)

func TestEmbeddedMigrationsPresent(t *testing.T) {
	entries, err := fs.ReadDir(FS, ".")
	if err != nil {
		t.Fatalf("read embed: %v", err)
	}
	if len(entries) == 0 {
		t.Fatalf("no migrations embedded")
	}
}
