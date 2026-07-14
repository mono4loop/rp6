// Package inspect captures semantic geometry from a Fyne canvas.
//
// Fyne exposes object geometry but does not provide stable automation IDs or a
// public semantic tree. Callers therefore register the meaningful objects under
// application-owned IDs, and Snapshot combines those IDs with their live layout
// geometry, layout visibility, clipping, accessibility metadata and state.
// Layout visibility means that the object and its ancestors are shown and that
// some of its bounds survive known clipping containers. It deliberately does
// not claim to detect opaque sibling/overlay occlusion.
package inspect

import (
	"fmt"
	"image"
	"math"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
)

const SchemaVersion = 1

// Target associates a stable semantic ID with a live Fyne object.
type Target struct {
	ID       string
	Kind     string
	Label    string
	Role     string
	Object   fyne.CanvasObject
	State    map[string]any
	Annotate bool
}

// Metadata describes the application state in which a snapshot was captured.
type Metadata struct {
	Scenario   string         `json:"scenario"`
	Variant    string         `json:"variant,omitempty"`
	Page       string         `json:"page,omitempty"`
	FormFactor string         `json:"formFactor,omitempty"`
	Backend    string         `json:"backend,omitempty"`
	State      map[string]any `json:"state,omitempty"`
	Notes      []string       `json:"notes,omitempty"`
}

// Size is a JSON-friendly logical size.
type Size struct {
	Width  float32 `json:"width"`
	Height float32 `json:"height"`
}

// PixelSize is a physical-pixel size.
type PixelSize struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// Rect is a logical-coordinate rectangle.
type Rect struct {
	X      float32 `json:"x"`
	Y      float32 `json:"y"`
	Width  float32 `json:"width"`
	Height float32 `json:"height"`
}

// PixelRect is a physical-pixel rectangle.
type PixelRect struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

// Canvas describes both coordinate spaces represented by a capture.
type Canvas struct {
	Logical Size      `json:"logical"`
	Pixel   PixelSize `json:"pixel"`
	Scale   float32   `json:"scale"`
}

// Element is one registered semantic target in a captured scene.
type Element struct {
	ID               string         `json:"id"`
	Kind             string         `json:"kind"`
	Label            string         `json:"label,omitempty"`
	Role             string         `json:"role,omitempty"`
	Present          bool           `json:"present"`
	SelfVisible      bool           `json:"selfVisible"`
	EffectiveVisible bool           `json:"effectiveVisible"`
	Occurrences      int            `json:"occurrences"`
	Duplicate        bool           `json:"duplicate"`
	Rect             Rect           `json:"rect"`
	VisibleRect      Rect           `json:"visibleRect"`
	PixelRect        PixelRect      `json:"pixelRect"`
	MinSize          Size           `json:"minSize"`
	UnderMin         bool           `json:"underMin"`
	Clipped          bool           `json:"clipped"`
	Annotated        bool           `json:"annotated,omitempty"`
	State            map[string]any `json:"state,omitempty"`
}

// Snapshot is the machine-readable companion to a rendered screenshot.
type Snapshot struct {
	Schema      int       `json:"schema"`
	Metadata    Metadata  `json:"metadata"`
	Canvas      Canvas    `json:"canvas"`
	ImageSHA256 string    `json:"imageSHA256,omitempty"`
	Elements    []Element `json:"elements"`
}

type placement struct {
	rect             Rect
	visibleRect      Rect
	selfVisible      bool
	effectiveVisible bool
}

// childProvider lets composite custom widgets expose semantic children without
// exposing renderer implementation details. components.RackPanel implements it.
type childProvider interface {
	InspectionChildren() []fyne.CanvasObject
}

// SnapshotCanvas records the current geometry of targets. Capture the canvas
// immediately before calling this function so custom-widget renderers have laid
// out their children.
func SnapshotCanvas(c fyne.Canvas, metadata Metadata, targets []Target) Snapshot {
	logical := c.Size()
	scale := c.Scale()
	canvasRect := Rect{Width: logical.Width, Height: logical.Height}
	placements := make(map[fyne.CanvasObject][]placement, len(targets))
	if root := c.Content(); root != nil {
		walk(root, fyne.Position{}, true, canvasRect, placements, map[fyne.CanvasObject]bool{})
	}

	out := Snapshot{
		Schema:   SchemaVersion,
		Metadata: metadata,
		Canvas: Canvas{
			Logical: Size{Width: logical.Width, Height: logical.Height},
			Pixel: PixelSize{
				Width:  pixelEdge(logical.Width, scale),
				Height: pixelEdge(logical.Height, scale),
			},
			Scale: scale,
		},
		Elements: make([]Element, 0, len(targets)),
	}
	for _, target := range targets {
		e := elementFor(target, placements[target.Object], c)
		out.Elements = append(out.Elements, e)
	}
	return out
}

func elementFor(target Target, found []placement, canvas fyne.Canvas) Element {
	label, role := target.Label, target.Role
	if accessible, ok := target.Object.(fyne.Accessible); ok {
		if label == "" {
			label = accessible.AccessibilityLabel()
		}
		if role == "" {
			role = string(accessible.AccessibilityRole())
		}
	}
	e := Element{
		ID:          target.ID,
		Kind:        target.Kind,
		Label:       label,
		Role:        role,
		Present:     len(found) > 0,
		Occurrences: len(found),
		Duplicate:   len(found) > 1,
		Annotated:   target.Annotate,
		State:       target.State,
	}
	if target.Object == nil || len(found) == 0 {
		return e
	}
	p := found[0]
	min := target.Object.MinSize()
	e.SelfVisible = p.selfVisible
	e.EffectiveVisible = p.effectiveVisible
	e.Rect = p.rect
	e.VisibleRect = p.visibleRect
	e.PixelRect = pixelRect(p.rect, canvas)
	e.MinSize = Size{Width: min.Width, Height: min.Height}
	e.UnderMin = p.effectiveVisible && (p.rect.Width+0.5 < min.Width || p.rect.Height+0.5 < min.Height)
	e.Clipped = p.selfVisible && !sameRect(p.rect, p.visibleRect)
	return e
}

func walk(obj fyne.CanvasObject, parent fyne.Position, ancestorsVisible bool, clip Rect, found map[fyne.CanvasObject][]placement, stack map[fyne.CanvasObject]bool) {
	if obj == nil || stack[obj] {
		return
	}
	stack[obj] = true
	defer delete(stack, obj)

	pos := parent.Add(obj.Position())
	size := obj.Size()
	r := Rect{X: pos.X, Y: pos.Y, Width: size.Width, Height: size.Height}
	visibleRect := intersect(r, clip)
	selfVisible := obj.Visible()
	effective := ancestorsVisible && selfVisible && visibleRect.Width > 0 && visibleRect.Height > 0
	found[obj] = append(found[obj], placement{
		rect:             r,
		visibleRect:      visibleRect,
		selfVisible:      selfVisible,
		effectiveVisible: effective,
	})

	childClip := clip
	switch obj.(type) {
	case *container.Scroll, *container.Split, *container.Clip:
		childClip = intersect(clip, r)
	}
	for _, child := range children(obj) {
		walk(child, pos, ancestorsVisible && selfVisible, childClip, found, stack)
	}
}

func children(obj fyne.CanvasObject) []fyne.CanvasObject {
	switch o := obj.(type) {
	case *fyne.Container:
		return o.Objects
	case *container.Split:
		return []fyne.CanvasObject{o.Leading, o.Trailing}
	case *container.Scroll:
		return []fyne.CanvasObject{o.Content}
	case *container.Clip:
		return []fyne.CanvasObject{o.Content}
	case *container.AppTabs:
		return tabChildren(o.Items)
	case *container.DocTabs:
		return tabChildren(o.Items)
	case *container.InnerWindow:
		return []fyne.CanvasObject{o.Content}
	case *container.MultipleWindows:
		children := make([]fyne.CanvasObject, len(o.Windows))
		for i, window := range o.Windows {
			children[i] = window
		}
		return children
	case *container.Navigation:
		// Navigation's internal stack is private. Root is still useful before
		// navigation starts; callers needing pushed pages should register a
		// semantic child provider around Navigation.
		return []fyne.CanvasObject{o.Root}
	case childProvider:
		return o.InspectionChildren()
	default:
		return nil
	}
}

func tabChildren(items []*container.TabItem) []fyne.CanvasObject {
	children := make([]fyne.CanvasObject, 0, len(items))
	for _, item := range items {
		if item != nil {
			children = append(children, item.Content)
		}
	}
	return children
}

func intersect(a, b Rect) Rect {
	x1 := max(a.X, b.X)
	y1 := max(a.Y, b.Y)
	x2 := min(a.X+a.Width, b.X+b.Width)
	y2 := min(a.Y+a.Height, b.Y+b.Height)
	if x2 <= x1 || y2 <= y1 {
		return Rect{X: x1, Y: y1}
	}
	return Rect{X: x1, Y: y1, Width: x2 - x1, Height: y2 - y1}
}

func sameRect(a, b Rect) bool {
	const tolerance = 0.5
	return abs(a.X-b.X) <= tolerance && abs(a.Y-b.Y) <= tolerance &&
		abs(a.Width-b.Width) <= tolerance && abs(a.Height-b.Height) <= tolerance
}

func pixelRect(r Rect, canvas fyne.Canvas) PixelRect {
	x1, y1 := canvas.PixelCoordinateForPosition(fyne.NewPos(r.X, r.Y))
	x2, y2 := canvas.PixelCoordinateForPosition(fyne.NewPos(r.X+r.Width, r.Y+r.Height))
	return PixelRect{
		X:      x1,
		Y:      y1,
		Width:  x2 - x1,
		Height: y2 - y1,
	}
}

func pixelEdge(v, scale float32) int {
	return int(math.Ceil(float64(v * scale)))
}

func abs(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// Find returns the element with id.
func (s Snapshot) Find(id string) (Element, bool) {
	for _, e := range s.Elements {
		if e.ID == id {
			return e, true
		}
	}
	return Element{}, false
}

// ImageBounds records the actual captured image dimensions. It is separate from
// Canvas.Pixel so callers can detect a scale/rounding mismatch in an artifact.
func ImageBounds(img image.Image) PixelSize {
	b := img.Bounds()
	return PixelSize{Width: b.Dx(), Height: b.Dy()}
}

func (r Rect) String() string {
	return fmt.Sprintf("x=%.1f y=%.1f w=%.1f h=%.1f", r.X, r.Y, r.Width, r.Height)
}
