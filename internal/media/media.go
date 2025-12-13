package media

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	_ "golang.org/x/image/webp"
)

const (
	VariantOriginal = "original"
	VariantContent  = "content"
	VariantThumb    = "thumb"
)

var ErrTooLarge = errors.New("upload too large")
var ErrInvalidImage = errors.New("invalid image")

// Manager handles filesystem operations for assets.
type Manager struct {
	root string
}

func NewManager(root string) *Manager {
	return &Manager{root: root}
}

// Save streams the upload to disk, computes SHA-256, validates pixels, and generates stub variants.
type SaveResult struct {
	SHA256 string
	Bytes  int64
	Mime   string
	Width  int
	Height int
	Ext    string
}

func (m *Manager) Save(ctx context.Context, r io.Reader, filename string, maxBytes int64, maxPixels int) (*SaveResult, error) {
	if err := os.MkdirAll(m.root, 0o755); err != nil {
		return nil, err
	}

	lim := &io.LimitedReader{R: r, N: maxBytes + 1}
	br := bufio.NewReader(lim)
	peek, _ := br.Peek(8192)
	mimeType := http.DetectContentType(peek)

	tmp, err := os.CreateTemp(m.root, "upload-*")
	if err != nil {
		return nil, err
	}
	defer func() {
		tmp.Close()
		os.Remove(tmp.Name())
	}()

	hash := sha256.New()
	mw := io.MultiWriter(tmp, hash)
	written, err := io.Copy(mw, br)
	if err != nil {
		return nil, err
	}
	if lim.N < 0 || written > maxBytes {
		return nil, ErrTooLarge
	}

	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	cfg, format, err := image.DecodeConfig(tmp)
	if err != nil {
		return nil, ErrInvalidImage
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > maxPixels {
		return nil, ErrInvalidImage
	}

	ext := strings.ToLower(filepath.Ext(filename))
	if ext == "" {
		if mimeExts, _ := mime.ExtensionsByType(mimeType); len(mimeExts) > 0 {
			ext = mimeExts[0]
		}
	}
	if ext == "" {
		// default to format-based extension
		ext = "." + format
	}
	shaHex := hex.EncodeToString(hash.Sum(nil))

	origPath := m.pathFor(shaHex, VariantOriginal, ext)
	if err := m.ensureDir(origPath); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp.Name(), origPath); err != nil {
		// maybe already exists, try copy
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if err := copyFile(tmp.Name(), origPath); err != nil {
			return nil, err
		}
	}

	if err := m.generateVariants(origPath, shaHex); err != nil {
		return nil, err
	}

	return &SaveResult{
		SHA256: shaHex,
		Bytes:  written,
		Mime:   mimeType,
		Width:  cfg.Width,
		Height: cfg.Height,
		Ext:    ext,
	}, nil
}

func (m *Manager) generateVariants(origPath, sha string) error {
	contentPath := m.pathFor(sha, VariantContent, ".webp")
	thumbPath := m.pathFor(sha, VariantThumb, ".webp")
	if err := m.ensureDir(contentPath); err != nil {
		return err
	}
	if err := m.ensureDir(thumbPath); err != nil {
		return err
	}
	// Stub generation: copy original to variants. Replace with libvips later.
	if err := copyIfMissing(origPath, contentPath); err != nil {
		return err
	}
	if err := copyIfMissing(origPath, thumbPath); err != nil {
		return err
	}
	return nil
}

func (m *Manager) ensureDir(path string) error {
	return os.MkdirAll(filepath.Dir(path), 0o755)
}

func copyIfMissing(src, dst string) error {
	if _, err := os.Stat(dst); err == nil {
		return nil
	}
	return copyFile(src, dst)
}

func copyFile(src, dst string) error {
	r, err := os.Open(src)
	if err != nil {
		return err
	}
	defer r.Close()
	w, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer w.Close()
	_, err = io.Copy(w, r)
	return err
}

func (m *Manager) pathFor(sha, variant, ext string) string {
	prefix1 := sha[0:2]
	prefix2 := sha[2:4]
	filename := sha + ext
	switch variant {
	case VariantOriginal:
		return filepath.Join(m.root, "original", prefix1, prefix2, filename)
	case VariantContent:
		return filepath.Join(m.root, "content", prefix1, prefix2, sha+".webp")
	case VariantThumb:
		return filepath.Join(m.root, "thumb", prefix1, prefix2, sha+".webp")
	default:
		return filepath.Join(m.root, variant, prefix1, prefix2, filename)
	}
}

func (m *Manager) PathForVariant(sha, variant, ext string) string {
	return m.pathFor(sha, variant, ext)
}

func (m *Manager) IsWritable() error {
	testPath := filepath.Join(m.root, ".writetest")
	if err := os.MkdirAll(m.root, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(testPath, []byte("ok"), 0o644); err != nil {
		return err
	}
	return os.Remove(testPath)
}
