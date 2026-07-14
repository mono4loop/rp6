package layoutlang

import (
	"image/color"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"

	"github.com/mono4loop/rp6/internal/ui/layoutspec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func rect() *canvas.Rectangle { return canvas.NewRectangle(color.White) }

// reg builds a registry with a stand-in widget for every given id.
func reg(ids ...string) layoutspec.Registry {
	r := layoutspec.Registry{}
	for _, id := range ids {
		r[id] = rect()
	}
	return r
}

func TestParseSimpleBorder(t *testing.T) {
	src := `
layout main {
  Border {
    top: transport;
    center: pads;
    bottom: status;
  }
}`
	doc, err := Parse(src)
	require.NoError(t, err)
	assert.Equal(t, []string{"main"}, doc.Names())

	node := doc.Select(Env{})
	obj := layoutspec.Build(reg("transport", "pads", "status"), node)
	c, ok := obj.(*fyne.Container)
	require.True(t, ok)
	assert.Len(t, c.Objects, 3)
}

func TestVariantSelectionByWidth(t *testing.T) {
	src := `
layout compact when width < 500 {
  VBox { pads; }
}
layout wide {
  HBox { pads; vu; }
}`
	doc, err := Parse(src)
	require.NoError(t, err)

	r := reg("pads", "vu")

	narrow := layoutspec.Build(r, doc.Select(Env{Nums: map[string]float64{"width": 400}}))
	nc := narrow.(*fyne.Container)
	assert.Len(t, nc.Objects, 1, "compact variant: just pads")

	wide := layoutspec.Build(r, doc.Select(Env{Nums: map[string]float64{"width": 900}}))
	wc := wide.(*fyne.Container)
	assert.Len(t, wc.Objects, 2, "wide variant: pads + vu")
}

func TestInlineIfDropsChild(t *testing.T) {
	src := `
layout main {
  VBox {
    transport;
    seq if !seq_docked;
  }
}`
	doc, err := Parse(src)
	require.NoError(t, err)
	r := reg("transport", "seq")

	withSeq := layoutspec.Build(r, doc.Select(Env{Bools: map[string]bool{"seq_docked": false}}))
	assert.Len(t, withSeq.(*fyne.Container).Objects, 2)

	docked := layoutspec.Build(r, doc.Select(Env{Bools: map[string]bool{"seq_docked": true}}))
	assert.Len(t, docked.(*fyne.Container).Objects, 1, "seq dropped when docked")
}

func TestSplitPropsAndRegions(t *testing.T) {
	src := `
layout main {
  Split {
    horizontal: true;
    offset: 0.5;
    leading: pads if pads_visible;
    trailing: seq if seq_docked;
  }
}`
	doc, err := Parse(src)
	require.NoError(t, err)
	r := reg("pads", "seq")

	// Both present -> a real split.
	both := layoutspec.Build(r, doc.Select(Env{Bools: map[string]bool{"pads_visible": true, "seq_docked": true}}))
	sp, ok := both.(*container.Split)
	require.True(t, ok)
	assert.True(t, sp.Horizontal)
	assert.InDelta(t, 0.5, sp.Offset, 0.0001)

	// Only pads -> collapses to pads.
	one := layoutspec.Build(r, doc.Select(Env{Bools: map[string]bool{"pads_visible": true}}))
	assert.Same(t, r["pads"], one)
}

func TestGridCols(t *testing.T) {
	src := `layout main { Grid { cols: 2; a; b; c; } }`
	doc, err := Parse(src)
	require.NoError(t, err)
	obj := layoutspec.Build(reg("a", "b", "c"), doc.Select(Env{}))
	c, ok := obj.(*fyne.Container)
	require.True(t, ok)
	assert.Len(t, c.Objects, 3)
}

func TestConditionExpressions(t *testing.T) {
	src := `
layout a when compact && width >= 300 { pads; }
layout b when !compact || height < 100 { vu; }
layout c { status; }`
	doc, err := Parse(src)
	require.NoError(t, err)

	// compact && width>=300 -> variant a (pads)
	got := doc.Select(Env{Bools: map[string]bool{"compact": true}, Nums: map[string]float64{"width": 400}})
	assert.Equal(t, layoutspec.Ref("pads"), got)

	// !compact -> variant b (vu)
	got = doc.Select(Env{Bools: map[string]bool{"compact": false}})
	assert.Equal(t, layoutspec.Ref("vu"), got)

	// compact but width<300 and height>=100 -> falls through to default c
	got = doc.Select(Env{Bools: map[string]bool{"compact": true}, Nums: map[string]float64{"width": 100, "height": 500}})
	assert.Equal(t, layoutspec.Ref("status"), got)
}

func TestNestedContainers(t *testing.T) {
	src := `
layout main {
  Border {
    top: VBox { transport; HBox { a; b; } };
    center: pads;
  }
}`
	doc, err := Parse(src)
	require.NoError(t, err)
	obj := layoutspec.Build(reg("transport", "a", "b", "pads"), doc.Select(Env{}))
	require.NotNil(t, obj)
	_, ok := obj.(*fyne.Container)
	require.True(t, ok)
}

func TestComments(t *testing.T) {
	src := `
// line comment
layout main { /* block */ VBox { pads; /* inline */ } }`
	doc, err := Parse(src)
	require.NoError(t, err)
	assert.Equal(t, []string{"main"}, doc.Names())
}

func TestParseErrors(t *testing.T) {
	cases := map[string]string{
		"missing layout kw":   `main { pads; }`,
		"unknown container":   `layout m { Frobnicate { a; } }`,
		"unclosed brace":      `layout m { VBox { pads; `,
		"bad region":          `layout m { Border { middle: pads; } }`,
		"positional in split": `layout m { Split { pads; } }`,
		"empty document":      ``,
		"bad condition":       `layout m when { pads; }`,
		"if on scalar prop":   `layout m { Grid { cols: 2 if compact; a; } }`,
		"page missing label":  `page play`,
		"non-layout in page":  `page p L { rack x { a } }`,
		"unclosed page block": `page p L { layout m { a }`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Parse(src)
			assert.Error(t, err, "expected a parse error")
		})
	}
}

func TestSpacer(t *testing.T) {
	src := `layout m { HBox { a; Spacer; b; } }`
	doc, err := Parse(src)
	require.NoError(t, err)
	obj := layoutspec.Build(reg("a", "b"), doc.Select(Env{}))
	c := obj.(*fyne.Container)
	assert.Len(t, c.Objects, 3, "a + spacer + b")
}

func TestSelectNoMatchIsNil(t *testing.T) {
	doc, err := Parse(`layout m when false { pads; }`)
	require.NoError(t, err)
	assert.Nil(t, doc.Select(Env{}))
}

func TestRackBlocks(t *testing.T) {
	src := `
layout main { pads }

rack transport {
  RackPanel { HBox { play; Separator; tempo; Spacer; badge } }
}

rack dlyrev {
  RackPanel { Grid { cols: 4; a; b; c; d } }
}`
	doc, err := Parse(src)
	require.NoError(t, err)
	assert.Equal(t, []string{"main"}, doc.Names())
	assert.ElementsMatch(t, []string{"transport", "dlyrev"}, doc.RackNames())

	// A defined rack block builds a RackPanel.
	obj := layoutspec.Build(reg("play", "tempo", "badge"), doc.Rack("transport", Env{}))
	require.NotNil(t, obj)

	// An undefined rack returns nil (caller keeps its own composition).
	assert.Nil(t, doc.Rack("nonexistent", Env{}))
}

func TestRackBlockConditions(t *testing.T) {
	doc, err := Parse(`
layout main { pads }
rack pads {
  Border {
    left: VBox { tools if show_tools }
    center: grid
  }
}`)
	require.NoError(t, err)
	r := reg("tools", "grid")

	withTools := layoutspec.Build(r, doc.Rack("pads", Env{Bools: map[string]bool{"show_tools": true}}))
	assert.Len(t, withTools.(*fyne.Container).Objects, 2, "tools + grid")

	noTools := layoutspec.Build(r, doc.Rack("pads", Env{Bools: map[string]bool{"show_tools": false}}))
	assert.Len(t, noTools.(*fyne.Container).Objects, 1, "grid only")
}

func TestSeparatorNode(t *testing.T) {
	doc, err := Parse(`layout m { HBox { a; Separator; b } }`)
	require.NoError(t, err)
	obj := layoutspec.Build(reg("a", "b"), doc.Select(Env{}))
	assert.Len(t, obj.(*fyne.Container).Objects, 3, "a + separator + b")
}

func TestRefProperties(t *testing.T) {
	doc, err := Parse(`layout m { VBox { vu(orientation: horizontal); status } }`)
	require.NoError(t, err)

	// The configurator receives the ref's id + properties.
	got := map[string]map[string]string{}
	obj := layoutspec.BuildConfig(reg("vu", "status"),
		func(id string, props map[string]string) { got[id] = props },
		doc.Select(Env{}))
	require.NotNil(t, obj)
	assert.Equal(t, map[string]string{"orientation": "horizontal"}, got["vu"])
	_, statusConfigured := got["status"]
	assert.False(t, statusConfigured, "a ref without properties isn't configured")
}

func TestRefPropertiesMultiple(t *testing.T) {
	doc, err := Parse(`layout m { widget(a: 1, b: two, c: true) }`)
	require.NoError(t, err)
	var props map[string]string
	layoutspec.BuildConfig(reg("widget"),
		func(_ string, p map[string]string) { props = p },
		doc.Select(Env{}))
	assert.Equal(t, map[string]string{"a": "1", "b": "two", "c": "true"}, props)
}

// TestPagesDeclared parses `page <id> <Label> { … }` blocks into ordered pages,
// each holding its own variants, while leaving rack parsing untouched.
func TestPagesDeclared(t *testing.T) {
	doc, err := Parse(`
page play PLAY {
  layout main { pads }
}
page loop LOOP {
  layout loop-main { rec }
}
rack pads { RackPanel { grid } }`)
	require.NoError(t, err)
	assert.Equal(t, []Page{{ID: "play", Label: "PLAY"}, {ID: "loop", Label: "LOOP"}}, doc.Pages())
	assert.Equal(t, []string{"main", "loop-main"}, doc.Names(), "variants flatten in page order")
	assert.ElementsMatch(t, []string{"pads"}, doc.RackNames())
}

// TestPageBlocksSelect drives the page mechanism: SelectForPage picks the first
// matching variant *within* the named page by form factor, and an unknown page
// falls back to the default/first-page variants.
func TestPageBlocksSelect(t *testing.T) {
	doc, err := Parse(`
page play PLAY {
  layout wide when width >= 500 { pads }
  layout narrow { vu }
}
page loop LOOP {
  layout main { rec }
}`)
	require.NoError(t, err)

	wide, ok := doc.SelectedNameForPage("play", Env{Nums: map[string]float64{"width": 600}})
	require.True(t, ok)
	assert.Equal(t, "wide", wide, "play page picks its wide variant")

	narrow, ok := doc.SelectedNameForPage("play", Env{Nums: map[string]float64{"width": 100}})
	require.True(t, ok)
	assert.Equal(t, "narrow", narrow, "play page falls to its default variant")

	loop, ok := doc.SelectedNameForPage("loop", Env{})
	require.True(t, ok)
	assert.Equal(t, "main", loop, "loop page picks its own variant")

	fallback, ok := doc.SelectedNameForPage("nope", Env{Nums: map[string]float64{"width": 600}})
	require.True(t, ok)
	assert.Equal(t, "wide", fallback, "unknown page falls back to the first page")
}

// TestPageBlockReopens checks a page declared twice with the same id merges its
// variants (in order) and appears once in Pages().
func TestPageBlockReopens(t *testing.T) {
	doc, err := Parse(`
page play PLAY { layout tablet when tablet { pads } }
page play PLAY { layout window { vu } }`)
	require.NoError(t, err)
	assert.Equal(t, []Page{{ID: "play", Label: "PLAY"}}, doc.Pages(), "reopened page appears once")
	assert.Equal(t, []string{"tablet", "window"}, doc.Names(), "variants merged in declaration order")

	name, ok := doc.SelectedNameForPage("play", Env{Bools: map[string]bool{"tablet": true}})
	require.True(t, ok)
	assert.Equal(t, "tablet", name)
}

// TestPagesEmptyByDefault confirms a document without `page` blocks has no pages
// (the app then runs single-page over the top-level variants).
func TestPagesEmptyByDefault(t *testing.T) {
	doc, err := Parse(`layout main { pads }`)
	require.NoError(t, err)
	assert.Empty(t, doc.Pages())
}
