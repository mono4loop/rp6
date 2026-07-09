package layoutspec

import (
	"image/color"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"github.com/mono4loop/rp6/internal/ui/components"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rect returns a distinct stand-in widget for a registry entry. A plain
// canvas.Rectangle needs no running app, so these tests are pure/headless.
func rect() *canvas.Rectangle { return canvas.NewRectangle(color.White) }

func TestRefResolves(t *testing.T) {
	r := rect()
	reg := Registry{"a": r}
	got := Build(reg, Ref("a"))
	assert.Same(t, r, got, "Ref should resolve to the registered object")
}

func TestRefMissingIsNil(t *testing.T) {
	assert.Nil(t, Build(Registry{}, Ref("nope")))
}

func TestRefWithConfigurator(t *testing.T) {
	r := rect()
	reg := Registry{"vu": r}

	var gotID string
	var gotProps map[string]string
	obj := BuildConfig(reg, func(id string, props map[string]string) {
		gotID, gotProps = id, props
	}, RefWith("vu", map[string]string{"orientation": "horizontal"}))

	assert.Same(t, r, obj, "still resolves to the registered object")
	assert.Equal(t, "vu", gotID)
	assert.Equal(t, map[string]string{"orientation": "horizontal"}, gotProps)
}

func TestRefWithoutPropsNotConfigured(t *testing.T) {
	called := false
	BuildConfig(Registry{"a": rect()}, func(string, map[string]string) { called = true }, Ref("a"))
	assert.False(t, called, "a plain Ref never invokes the configurator")
}

func TestBuildNilNode(t *testing.T) {
	assert.Nil(t, Build(Registry{}, nil))
}

func TestVBoxDropsNilChildren(t *testing.T) {
	reg := Registry{"a": rect(), "b": rect()}
	// A Ref to a missing id resolves to nil and is dropped.
	n := VBox(Ref("a"), Ref("missing"), Ref("b"))
	obj := Build(reg, n)
	box, ok := obj.(*fyne.Container)
	require.True(t, ok, "VBox should build a *fyne.Container")
	assert.Len(t, box.Objects, 2, "only the two resolvable children survive")
}

func TestEmptyBoxIsNil(t *testing.T) {
	assert.Nil(t, Build(Registry{}, VBox(Ref("missing"))))
	assert.Nil(t, Build(Registry{}, HBox()))
}

func TestBorderRegionsAndCenter(t *testing.T) {
	reg := Registry{"top": rect(), "center": rect(), "right": rect()}
	n := Border{
		Top:    Ref("top"),
		Right:  Ref("right"),
		Bottom: Ref("bottom"), // missing -> omitted
		Center: []Node{Ref("center"), Ref("missing")},
	}
	obj := Build(reg, n)
	c, ok := obj.(*fyne.Container)
	require.True(t, ok)
	// Border keeps only the non-nil edges + surviving center children: top,
	// right and one center object = 3 total.
	assert.Len(t, c.Objects, 3)
}

func TestSplitCollapsesWhenOneSideMissing(t *testing.T) {
	r := rect()
	reg := Registry{"a": r}
	// Trailing missing -> the split collapses to the leading object alone.
	got := Build(reg, Split{Horizontal: true, Leading: Ref("a"), Trailing: Ref("gone")})
	assert.Same(t, r, got)

	// Both present -> a real split.
	reg["b"] = rect()
	got = Build(reg, Split{Horizontal: true, Leading: Ref("a"), Trailing: Ref("b"), Offset: 0.5})
	sp, ok := got.(*container.Split)
	require.True(t, ok)
	assert.InDelta(t, 0.5, sp.Offset, 0.0001)
	assert.True(t, sp.Horizontal)

	// Neither present -> nil.
	assert.Nil(t, Build(Registry{}, Split{Leading: Ref("x"), Trailing: Ref("y")}))
}

func TestGrid(t *testing.T) {
	reg := Registry{"a": rect(), "b": rect(), "c": rect()}
	obj := Build(reg, Grid{Cols: 2, Children: []Node{Ref("a"), Ref("b"), Ref("c")}})
	c, ok := obj.(*fyne.Container)
	require.True(t, ok)
	assert.Len(t, c.Objects, 3)
}

func TestSpacer(t *testing.T) {
	obj := Build(Registry{}, Spacer())
	// A spacer is layout.NewSpacer()'s object; assert it's the spacer type by
	// checking it satisfies the SpacerObject marker interface Fyne uses.
	_, ok := obj.(layout.SpacerObject)
	assert.True(t, ok)
}

func TestSeparator(t *testing.T) {
	obj := Build(Registry{}, Separator())
	_, ok := obj.(*widget.Separator)
	assert.True(t, ok)
}

func TestRackPanel(t *testing.T) {
	reg := Registry{"a": rect(), "b": rect()}

	// Single child -> wrapped directly.
	obj := Build(reg, RackPanel(HBox(Ref("a"), Ref("b"))))
	_, ok := obj.(*components.RackPanel)
	require.True(t, ok, "RackPanel should build a *components.RackPanel")

	// No surviving children -> nil.
	assert.Nil(t, Build(Registry{}, RackPanel(Ref("missing"))))
}
