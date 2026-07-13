package inspect

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
)

// Bundle contains a clean screenshot, an annotated screenshot and their
// semantic manifest.
type Bundle struct {
	Snapshot  Snapshot
	Clean     image.Image
	Annotated image.Image
}

// CaptureBundle captures a clean scene, snapshots its semantic geometry, then
// captures a temporary labeled overlay. The live canvas is restored afterwards.
func CaptureBundle(c fyne.Canvas, metadata Metadata, targets []Target) (Bundle, error) {
	clean := c.Capture()
	snapshot := SnapshotCanvas(c, metadata, targets)
	// The captured image is authoritative. This also avoids drift if a Fyne
	// driver changes edge rounding for fractional logical sizes.
	snapshot.Canvas.Pixel = ImageBounds(clean)

	cleanPNG, err := encodePNG(clean)
	if err != nil {
		return Bundle{}, fmt.Errorf("inspect: encode clean capture: %w", err)
	}
	hash := sha256.Sum256(cleanPNG)
	snapshot.ImageSHA256 = hex.EncodeToString(hash[:])

	overlay := annotationOverlay(snapshot)
	c.Overlays().Add(overlay)
	annotated := c.Capture()
	c.Overlays().Remove(overlay)
	return Bundle{Snapshot: snapshot, Clean: clean, Annotated: annotated}, nil
}

// WriteBundle writes <name>.json, <name>.png and <name>-annotated.png.
func WriteBundle(dir, name string, bundle Bundle) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("inspect: create artifact directory: %w", err)
	}
	name = safeName(name)
	manifest, err := json.MarshalIndent(bundle.Snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("inspect: encode manifest: %w", err)
	}
	manifest = append(manifest, '\n')
	if err := os.WriteFile(filepath.Join(dir, name+".json"), manifest, 0o644); err != nil {
		return fmt.Errorf("inspect: write manifest: %w", err)
	}
	if err := writePNG(filepath.Join(dir, name+".png"), bundle.Clean); err != nil {
		return err
	}
	if err := writePNG(filepath.Join(dir, name+"-annotated.png"), bundle.Annotated); err != nil {
		return err
	}
	return nil
}

func annotationOverlay(snapshot Snapshot) fyne.CanvasObject {
	objects := make([]fyne.CanvasObject, 0, len(snapshot.Elements)*3)
	for i, e := range snapshot.Elements {
		if !e.Annotated || !e.EffectiveVisible {
			continue
		}
		accent := annotationColor(i, e.Clipped || e.UnderMin)
		outline := canvas.NewRectangle(color.Transparent)
		outline.StrokeColor = accent
		outline.StrokeWidth = 3
		outline.Move(fyne.NewPos(e.Rect.X, e.Rect.Y))
		outline.Resize(fyne.NewSize(e.Rect.Width, e.Rect.Height))
		objects = append(objects, outline)

		text := canvas.NewText(e.ID, color.White)
		text.TextSize = 13
		text.TextStyle = fyne.TextStyle{Bold: true, Monospace: true}
		textSize := text.MinSize()
		x := max(float32(0), e.Rect.X+4)
		y := max(float32(0), e.Rect.Y+3)
		if x+textSize.Width+8 > snapshot.Canvas.Logical.Width {
			x = max(float32(0), snapshot.Canvas.Logical.Width-textSize.Width-8)
		}
		if y+textSize.Height+6 > snapshot.Canvas.Logical.Height {
			y = max(float32(0), snapshot.Canvas.Logical.Height-textSize.Height-6)
		}
		bg := canvas.NewRectangle(color.NRGBA{A: 0xD8})
		bg.CornerRadius = 2
		bg.Move(fyne.NewPos(x-3, y-2))
		bg.Resize(fyne.NewSize(textSize.Width+6, textSize.Height+4))
		text.Move(fyne.NewPos(x, y))
		text.Resize(textSize)
		objects = append(objects, bg, text)
	}
	return container.NewWithoutLayout(objects...)
}

func annotationColor(index int, bad bool) color.NRGBA {
	if bad {
		return color.NRGBA{R: 0xFF, G: 0x34, B: 0x34, A: 0xFF}
	}
	palette := [...]color.NRGBA{
		{R: 0x00, G: 0xE5, B: 0xFF, A: 0xFF},
		{R: 0xFF, G: 0xD5, B: 0x00, A: 0xFF},
		{R: 0x76, G: 0xFF, B: 0x03, A: 0xFF},
		{R: 0xFF, G: 0x40, B: 0x81, A: 0xFF},
		{R: 0xB3, G: 0x88, B: 0xFF, A: 0xFF},
	}
	return palette[index%len(palette)]
}

func writePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("inspect: create %s: %w", path, err)
	}
	if err := png.Encode(f, img); err != nil {
		_ = f.Close()
		return fmt.Errorf("inspect: encode %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("inspect: close %s: %w", path, err)
	}
	return nil
}

func encodePNG(img image.Image) ([]byte, error) {
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func safeName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r), r == '-', r == '_':
			b.WriteRune(r)
		case unicode.IsSpace(r):
			b.WriteByte('-')
		}
	}
	name := strings.Trim(b.String(), "-_")
	if name == "" {
		return "layout"
	}
	return name
}
