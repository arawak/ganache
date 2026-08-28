package media

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
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

	"github.com/gen2brain/webp"
	"golang.org/x/image/draw"
)

const (
	VariantOriginal = "original"
	VariantContent  = "content"
	VariantThumb    = "thumb"
)

var ErrTooLarge = errors.New("upload too large")
var ErrInvalidImage = errors.New("invalid image")

// VariantConfig controls the dimensions of generated media variants.
type VariantConfig struct {
	ContentMaxWidth int
	ThumbMaxWidth   int
}

// Manager handles filesystem operations for assets.
type Manager struct {
	root     string
	variants VariantConfig
}

func NewManager(root string, variants VariantConfig) *Manager {
	return &Manager{root: root, variants: variants}
}

// SaveResult describes a validated, persisted upload.
type SaveResult struct {
	SHA256 string
	Bytes  int64
	Mime   string
	Width  int
	Height int
	Ext    string
}

// Save streams the upload to disk, computes SHA-256, validates pixels, and generates WebP variants.
func (m *Manager) Save(ctx context.Context, r io.Reader, filename string, maxBytes int64, maxPixels int) (*SaveResult, error) {
	if err := os.MkdirAll(m.root, 0o755); err != nil {
		return nil, err
	}

	lim := &io.LimitedReader{R: r, N: maxBytes + 1}
	br := bufio.NewReader(lim)
	peek, peekErr := br.Peek(512)
	if peekErr != nil && !errors.Is(peekErr, io.EOF) {
		return nil, fmt.Errorf("failed to peek for MIME detection: %w", peekErr)
	}
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
	if written > maxBytes {
		return nil, ErrTooLarge
	}

	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	cfg, format, err := image.DecodeConfig(tmp)
	if err != nil {
		return nil, ErrInvalidImage
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > int64(maxPixels) {
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

	var renamed bool
	if err := os.Rename(tmp.Name(), origPath); err != nil {
		if copyErr := copyFile(tmp.Name(), origPath); copyErr != nil {
			return nil, fmt.Errorf("failed to move file to destination: rename failed (%w), copy failed (%v)", err, copyErr)
		}
	} else {
		renamed = true
	}

	if err := m.generateVariants(origPath, shaHex); err != nil {
		if renamed {
			os.Remove(origPath)
		}
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
	original, err := os.Open(origPath)
	if err != nil {
		return err
	}
	imageData, _, decodeErr := image.Decode(original)
	closeErr := original.Close()
	if decodeErr != nil {
		return fmt.Errorf("decode original for variants: %w", decodeErr)
	}
	if closeErr != nil {
		return closeErr
	}

	variants := []struct {
		name     string
		maxWidth int
	}{
		{name: VariantContent, maxWidth: m.variants.ContentMaxWidth},
		{name: VariantThumb, maxWidth: m.variants.ThumbMaxWidth},
	}
	for _, variant := range variants {
		if variant.maxWidth <= 0 {
			return fmt.Errorf("%s max width must be positive", variant.name)
		}
		path := m.pathFor(sha, variant.name, ".webp")
		if err := m.writeVariant(path, resizeToMaxWidth(imageData, variant.maxWidth)); err != nil {
			return fmt.Errorf("generate %s variant: %w", variant.name, err)
		}
	}
	return nil
}

func resizeToMaxWidth(source image.Image, maxWidth int) image.Image {
	bounds := source.Bounds()
	if bounds.Dx() <= maxWidth {
		return source
	}

	height := int((int64(bounds.Dy())*int64(maxWidth) + int64(bounds.Dx())/2) / int64(bounds.Dx()))
	if height < 1 {
		height = 1
	}
	resized := image.NewNRGBA(image.Rect(0, 0, maxWidth, height))
	draw.CatmullRom.Scale(resized, resized.Bounds(), source, bounds, draw.Over, nil)
	return resized
}

func (m *Manager) writeVariant(path string, source image.Image) error {
	if err := m.ensureDir(path); err != nil {
		return err
	}

	temporary, err := os.CreateTemp(filepath.Dir(path), ".variant-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
	}()

	if err := webp.Encode(temporary, source, webp.Options{Quality: 82, Method: 4}); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

func (m *Manager) ensureDir(path string) error {
	return os.MkdirAll(filepath.Dir(path), 0o755)
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
