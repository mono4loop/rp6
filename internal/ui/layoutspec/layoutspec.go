// Package layoutspec is a small, declarative description of a Fyne layout tree.
//
// It is a "Go-data DSL": instead of assembling container.New… calls imperatively,
// you describe the arrangement as a tree of Nodes that reference pre-built
// widgets by ID, then Build turns that tree into real fyne.CanvasObjects. The
// point is to keep *structure and placement* separate from *behavior*: the
// widgets (and all their callbacks) are constructed and wired in ordinary Go,
// registered under stable IDs, and the spec only says where they go.
//
// It is intentionally generic — like the rest of internal/ui, it knows nothing
// about the P-6, MIDI, or any specific application. It imports only Fyne.
//
// A Ref may carry string properties (RefWith); when it does, the Configurator
// passed to BuildConfig is invoked with them so the application can apply
// device-specific settings to the widget (e.g. a meter's orientation) before it
// is placed. layoutspec itself doesn't interpret properties — it stays generic.
//
// layoutspec is the compile target for the text layout language (see the sibling
// layoutlang package), which parses a file into these nodes and picks between
// form-factor variants; layoutspec itself is just the builder.
package layoutspec

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"github.com/mono4loop/rp6/internal/ui/components"
)

// Registry maps a component ID to the concrete widget it stands for. The
// application builds this from its already-wired widgets; the spec's Ref nodes
// resolve against it. A missing ID resolves to nil and is skipped, so a spec can
// safely reference optional components.
type Registry map[string]fyne.CanvasObject

// Configurator applies a Ref's properties to the widget it resolves to, before
// the widget is placed. It's supplied by the application (which knows what a
// property like "orientation" means for a given id) — layoutspec is generic and
// only carries the strings through.
type Configurator func(id string, props map[string]string)

// buildCtx carries the registry and (optional) configurator down the node tree
// as it's realized.
type buildCtx struct {
	reg       Registry
	configure Configurator
}

// Node is one element of a layout tree: either a reference to a registered
// widget (Ref/RefWith) or a container of further nodes (VBox, HBox, Stack,
// Border, Split, Grid, RackPanel). build is unexported so the set of node kinds
// stays closed to this package.
type Node interface {
	build(*buildCtx) fyne.CanvasObject
}

// Build realizes a node tree, resolving Refs against reg. A nil node yields nil.
// Ref properties are ignored (use BuildConfig to handle them).
func Build(reg Registry, n Node) fyne.CanvasObject {
	return BuildConfig(reg, nil, n)
}

// BuildConfig is Build with a Configurator: when a Ref carries properties, the
// configurator is called with (id, props) so the caller can apply them to the
// resolved widget before it's placed.
func BuildConfig(reg Registry, configure Configurator, n Node) fyne.CanvasObject {
	if n == nil {
		return nil
	}
	return n.build(&buildCtx{reg: reg, configure: configure})
}

// buildNode realizes a single (possibly nil) node.
func buildNode(ctx *buildCtx, n Node) fyne.CanvasObject {
	if n == nil {
		return nil
	}
	return n.build(ctx)
}

// buildChildren realizes a slice of child nodes, dropping any that resolve to
// nil (nil node, or a Ref to a missing/nil widget) so containers never receive a
// nil object.
func buildChildren(ctx *buildCtx, nodes []Node) []fyne.CanvasObject {
	out := make([]fyne.CanvasObject, 0, len(nodes))
	for _, n := range nodes {
		if o := buildNode(ctx, n); o != nil {
			out = append(out, o)
		}
	}
	return out
}

// refNode references a registered widget by ID, with optional properties applied
// via the Configurator.
type refNode struct {
	id    string
	props map[string]string
}

// Ref references the registered widget with the given ID. It resolves to nil
// (and is skipped) if the ID isn't in the Registry.
func Ref(id string) Node { return refNode{id: id} }

// RefWith is Ref with properties handed to the Configurator (see BuildConfig).
func RefWith(id string, props map[string]string) Node { return refNode{id: id, props: props} }

func (r refNode) build(ctx *buildCtx) fyne.CanvasObject {
	obj := ctx.reg[r.id]
	if obj != nil && len(r.props) > 0 && ctx.configure != nil {
		ctx.configure(r.id, r.props)
	}
	return obj
}

// boxNode is a VBox (vertical) or HBox (horizontal) of children.
type boxNode struct {
	vertical bool
	children []Node
}

// VBox stacks its children vertically (container.NewVBox). Nil/omitted children
// are dropped; if none remain it resolves to nil.
func VBox(children ...Node) Node { return boxNode{vertical: true, children: children} }

// HBox lays its children out horizontally (container.NewHBox). Nil/omitted
// children are dropped; if none remain it resolves to nil.
func HBox(children ...Node) Node { return boxNode{vertical: false, children: children} }

func (b boxNode) build(ctx *buildCtx) fyne.CanvasObject {
	objs := buildChildren(ctx, b.children)
	if len(objs) == 0 {
		return nil
	}
	if b.vertical {
		return container.NewVBox(objs...)
	}
	return container.NewHBox(objs...)
}

// stackNode overlays its children (container.NewStack).
type stackNode struct{ children []Node }

// Stack overlays its children, each filling the whole area (container.NewStack).
func Stack(children ...Node) Node { return stackNode{children: children} }

func (s stackNode) build(ctx *buildCtx) fyne.CanvasObject {
	objs := buildChildren(ctx, s.children)
	if len(objs) == 0 {
		return nil
	}
	return container.NewStack(objs...)
}

// spacerNode is an expanding gap (layout.NewSpacer) used inside a box to push
// siblings apart.
type spacerNode struct{}

// Spacer is an expanding blank that pushes surrounding box children apart
// (layout.NewSpacer). Only meaningful inside a VBox/HBox.
func Spacer() Node { return spacerNode{} }

func (spacerNode) build(*buildCtx) fyne.CanvasObject { return layout.NewSpacer() }

// separatorNode is a thin divider line (widget.NewSeparator), used between
// items in a box.
type separatorNode struct{}

// Separator is a thin divider line (widget.NewSeparator), e.g. between controls
// in a toolbar.
func Separator() Node { return separatorNode{} }

func (separatorNode) build(*buildCtx) fyne.CanvasObject { return widget.NewSeparator() }

// rackPanelNode wraps its content in a gunmetal rack-unit frame
// (components.RackPanel).
type rackPanelNode struct{ children []Node }

// RackPanel wraps its content in a gunmetal rack-unit frame. It normally has a
// single child (a box/border laying out the rack's controls); multiple children
// are stacked vertically. Resolves to nil if it has no surviving children.
func RackPanel(children ...Node) Node { return rackPanelNode{children: children} }

func (r rackPanelNode) build(ctx *buildCtx) fyne.CanvasObject {
	objs := buildChildren(ctx, r.children)
	switch len(objs) {
	case 0:
		return nil
	case 1:
		return components.NewRackPanel(objs[0])
	default:
		return components.NewRackPanel(container.NewVBox(objs...))
	}
}

// Border arranges an optional widget on each edge with the remaining space
// filled by Center (container.NewBorder). Any region may be nil/omitted. Center
// is a slice so multiple overlaid objects (or none) are allowed.
type Border struct {
	Top, Bottom, Left, Right Node
	Center                   []Node
}

func (b Border) build(ctx *buildCtx) fyne.CanvasObject {
	top := buildNode(ctx, b.Top)
	bottom := buildNode(ctx, b.Bottom)
	left := buildNode(ctx, b.Left)
	right := buildNode(ctx, b.Right)
	center := buildChildren(ctx, b.Center)
	return container.NewBorder(top, bottom, left, right, center...)
}

// Split is a draggable two-pane split (container.NewHSplit / NewVSplit). Offset
// is the leading pane's fraction (0..1; 0 uses Fyne's default of 0.5). If only
// one side resolves it collapses to that side; if neither does, to nil.
type Split struct {
	Horizontal        bool
	Leading, Trailing Node
	Offset            float64
}

func (s Split) build(ctx *buildCtx) fyne.CanvasObject {
	lead := buildNode(ctx, s.Leading)
	trail := buildNode(ctx, s.Trailing)
	switch {
	case lead == nil && trail == nil:
		return nil
	case lead == nil:
		return trail
	case trail == nil:
		return lead
	}
	var sp *container.Split
	if s.Horizontal {
		sp = container.NewHSplit(lead, trail)
	} else {
		sp = container.NewVSplit(lead, trail)
	}
	if s.Offset > 0 {
		sp.SetOffset(s.Offset)
	}
	return sp
}

// Grid arranges children in a fixed number of columns (container.New with a
// grid layout). Nil/omitted children are dropped; if none remain it resolves to
// nil.
type Grid struct {
	Cols     int
	Children []Node
}

func (g Grid) build(ctx *buildCtx) fyne.CanvasObject {
	objs := buildChildren(ctx, g.Children)
	if len(objs) == 0 {
		return nil
	}
	cols := g.Cols
	if cols < 1 {
		cols = 1
	}
	return container.NewGridWithColumns(cols, objs...)
}
