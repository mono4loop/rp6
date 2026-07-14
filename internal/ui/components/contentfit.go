package components

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
)

// FitSizeFunc returns the preferred child size for an available allocation.
type FitSizeFunc func(available fyne.Size) fyne.Size

// ContentFit is a transparent one-child wrapper that lets its content consume
// less than the size offered by the parent. It is top-aligned and horizontally
// centered so any excess viewport space remains outside the fitted component.
type ContentFit struct {
	widget.BaseWidget
	Content    fyne.CanvasObject
	FitSize    FitSizeFunc
	contentMin bool
	minSize    func() fyne.Size
	expandH    bool
	expandV    bool
}

// SetExpand makes the fitted content fill the available width and/or height
// (instead of shrinking to its FitSize). Only the wrapped object grows — a rack
// panel expanding this way still centers its own children at their natural size.
// It doesn't change MinSize, so expanding never forces the parent larger.
func (f *ContentFit) SetExpand(horizontal, vertical bool) {
	if f.expandH == horizontal && f.expandV == vertical {
		return
	}
	f.expandH, f.expandV = horizontal, vertical
	f.Refresh()
}

// SetMinSizeFunc supplies a dynamic minimum for parent layout negotiation.
func (f *ContentFit) SetMinSizeFunc(minSize func() fyne.Size) {
	f.minSize = minSize
	f.Refresh()
}

// SetContentMin controls whether the wrapper advertises its content's dynamic
// minimum to parent layouts. Enable this for edge/VBox racks that need a real
// footprint; leave it off for adaptive center content that may shrink.
func (f *ContentFit) SetContentMin(on bool) {
	f.contentMin = on
	f.Refresh()
}

// NewContentFit wraps content and sizes it with fitSize on every layout pass.
func NewContentFit(content fyne.CanvasObject, fitSize FitSizeFunc) *ContentFit {
	f := &ContentFit{Content: content, FitSize: fitSize}
	f.ExtendBaseWidget(f)
	return f
}

// InspectionChildren exposes the fitted content without renderer coupling.
func (f *ContentFit) InspectionChildren() []fyne.CanvasObject {
	return []fyne.CanvasObject{f.Content}
}

// AccessibilityLabel names the transparent grouping wrapper.
func (*ContentFit) AccessibilityLabel() string { return "Fitted content" }

// AccessibilityRole reports that this wrapper groups related content.
func (*ContentFit) AccessibilityRole() fyne.AccessibleRole {
	return fyne.AccessibleRoleContainer
}

func (f *ContentFit) CreateRenderer() fyne.WidgetRenderer {
	return &contentFitRenderer{fit: f}
}

type contentFitRenderer struct{ fit *ContentFit }

func (*contentFitRenderer) Destroy() {}

func (r *contentFitRenderer) Layout(size fyne.Size) {
	target := r.fit.Content.MinSize()
	if r.fit.FitSize != nil {
		target = r.fit.FitSize(size)
	}
	if r.fit.expandH {
		target.Width = size.Width
	}
	if r.fit.expandV {
		target.Height = size.Height
	}
	target.Width = min(max(target.Width, float32(0)), size.Width)
	target.Height = min(max(target.Height, float32(0)), size.Height)
	r.fit.Content.Move(fyne.NewPos((size.Width-target.Width)/2, 0))
	r.fit.Content.Resize(target)
}

// MinSize is deliberately small by default so preferred physical sizes do not
// force the window larger. SetContentMin or SetMinSizeFunc opts into a real
// parent-negotiated minimum. The child is clamped to the available allocation;
// responsive children can reflow and overflow access needs its own scroller.
func (r *contentFitRenderer) MinSize() fyne.Size {
	if r.fit.minSize != nil {
		return r.fit.minSize()
	}
	if r.fit.contentMin {
		return r.fit.Content.MinSize()
	}
	return fyne.NewSize(1, 1)
}

func (r *contentFitRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.fit.Content}
}

func (r *contentFitRenderer) Refresh() {
	// FitSize may depend on live component state (visible bars, pad density), so
	// refresh must recompute the child allocation even when the wrapper did not
	// itself resize.
	r.Layout(r.fit.Size())
	r.fit.Content.Refresh()
}
