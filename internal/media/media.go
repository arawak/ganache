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

	"github.com/disintegration/imaging"
	"github.com/gen2brain/webp"
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
	if err := m.validateVariantConfig(); err != nil {
		return nil, err
	}
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
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
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

	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	imageData, err := imaging.Decode(tmp, imaging.AutoOrientation(true))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidImage, err)
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
	createdOriginal, err := installOriginal(tmp.Name(), origPath)
	if err != nil {
		return nil, err
	}

	if err := m.generateVariants(imageData, shaHex); err != nil {
		if createdOriginal {
			_ = os.Remove(origPath)
		}
		return nil, err
	}

	orientedBounds := imageData.Bounds()
	return &SaveResult{
		SHA256: shaHex,
		Bytes:  written,
		Mime:   mimeType,
		Width:  orientedBounds.Dx(),
		Height: orientedBounds.Dy(),
		Ext:    ext,
	}, nil
}

func (m *Manager) generateVariants(imageData image.Image, sha string) error {
	variants := []struct {
		name     string
		maxWidth int
	}{
		{name: VariantContent, maxWidth: m.variants.ContentMaxWidth},
		{name: VariantThumb, maxWidth: m.variants.ThumbMaxWidth},
	}

	type stagedVariant struct {
		target    string
		temporary string
	}
	staged := make([]stagedVariant, 0, len(variants))
	defer func() {
		for _, variant := range staged {
			if variant.temporary != "" {
				_ = os.Remove(variant.temporary)
			}
		}
	}()

	for _, variant := range variants {
		target := m.pathFor(sha, variant.name, ".webp")
		temporary, err := m.stageVariant(target, resizeToMaxWidth(imageData, variant.maxWidth))
		if err != nil {
			return fmt.Errorf("generate %s variant: %w", variant.name, err)
		}
		staged = append(staged, stagedVariant{target: target, temporary: temporary})
	}

	for index := range staged {
		if err := os.Rename(staged[index].temporary, staged[index].target); err != nil {
			return fmt.Errorf("publish %s variant: %w", variants[index].name, err)
		}
		staged[index].temporary = ""
	}
	return nil
}

func resizeToMaxWidth(source image.Image, maxWidth int) image.Image {
	if source.Bounds().Dx() <= maxWidth {
		return source
	}
	return imaging.Resize(source, maxWidth, 0, imaging.CatmullRom)
}

func (m *Manager) stageVariant(path string, source image.Image) (string, error) {
	if err := m.ensureDir(path); err != nil {
		return "", err
	}

	temporary, err := os.CreateTemp(filepath.Dir(path), ".variant-*")
	if err != nil {
		return "", err
	}
	temporaryPath := temporary.Name()
	complete := false
	defer func() {
		if !complete {
			_ = temporary.Close()
			_ = os.Remove(temporaryPath)
		}
	}()

	if err := webp.Encode(temporary, source, webp.Options{Quality: 82, Method: 4}); err != nil {
		return "", err
	}
	if err := temporary.Sync(); err != nil {
		return "", err
	}
	if err := temporary.Close(); err != nil {
		return "", err
	}
	complete = true
	return temporaryPath, nil
}

func (m *Manager) validateVariantConfig() error {
	if m.variants.ContentMaxWidth <= 0 {
		return fmt.Errorf("%s max width must be positive", VariantContent)
	}
	if m.variants.ThumbMaxWidth <= 0 {
		return fmt.Errorf("%s max width must be positive", VariantThumb)
	}
	return nil
}

func installOriginal(temporaryPath, originalPath string) (bool, error) {
	err := os.Link(temporaryPath, originalPath)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrExist) {
		return false, nil
	}
	return false, fmt.Errorf("install original: %w", err)
}

func (m *Manager) ensureDir(path string) error {
	return os.MkdirAll(filepath.Dir(path), 0o755)
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
