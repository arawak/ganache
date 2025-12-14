package httpapi

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAPIKeysSuccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keys.yaml")
	yaml := `
- id: one
  key: secret1
  permissions:
    - can_search
    - can_upload
- id: two
  key: secret2
  permissions:
    - can_search
`
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	store, err := LoadAPIKeys(path)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}

	if _, ok := store.Lookup("secret1"); !ok {
		t.Fatalf("expected to find secret1")
	}
	if _, ok := store.Lookup("secret2"); !ok {
		t.Fatalf("expected to find secret2")
	}
}

func TestLoadAPIKeysDuplicate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keys.yaml")
	yaml := `
- id: one
  key: dup
  permissions: [can_search]
- id: two
  key: dup
  permissions: [can_search]
`
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	if _, err := LoadAPIKeys(path); err == nil {
		t.Fatalf("expected error for duplicate keys")
	}
}

func TestLoadAPIKeysEmptyPermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keys.yaml")
	yaml := `
- id: one
  key: secret
  permissions: []
`
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	if _, err := LoadAPIKeys(path); err == nil {
		t.Fatalf("expected error for empty permissions")
	}
}

func TestLoadAPIKeysMissingFile(t *testing.T) {
	if _, err := LoadAPIKeys(filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Fatalf("expected error for missing file")
	}
}
