// Package layoutlang parses a small, Blueprint-inspired text language that
// describes Fyne layouts, and compiles it to the layoutspec node tree. It is the
// runtime, file-editable front-end to layoutspec: an application can ship a
// default layout, let the user edit a plain-text file to rearrange the UI, and
// reload it without recompiling.
//
// # Language
//
// A document is a sequence of named layout variants, each an arrangement guarded
// by an optional `when` condition. The first variant whose condition matches the
// Env (see Select) is used, so order matters — put specific variants first and
// an unguarded default last:
//
//	layout compact when width < 500 {
//	    Border {
//	        top:    VBox { transport; seq if !seq_docked; }
//	        center: pads if pads_visible;
//	        bottom: VBox { vu; status; }
//	    }
//	}
//
//	layout wide {
//	    Border {
//	        top:    VBox { transport; }
//	        center: Split { leading: pads; trailing: seq if seq_docked; }
//	        right:  vu;
//	        bottom: status;
//	    }
//	}
//
// Nodes are either a container (VBox, HBox, Stack, Border, Split, Grid) written
// `Type { … }`, the bare keyword `Spacer`, or a widget reference — any other
// bare identifier, resolved against the Registry at build time.
//
// A document may also declare ordered application pages with
// `page <id> <Label> { <layout variants…> }` (e.g. `page play PLAY { … }`). Each
// page block holds its own `layout` variants; the host selects a page by id
// (SelectForPage) and then the first matching variant within it, so a page's
// arrangement is fully defined in the layout file (no external flag). Variants
// declared outside any page block form an implicit default page. See Pages/Page.
//
// Container bodies hold entries terminated by `;`. Box/Stack/Grid take
// positional children; Border and Split take named regions (top/bottom/left/
// right/center and leading/trailing). Grid takes a `cols:` number; Split takes
// `horizontal:` (bool) and `offset:` (0..1). Any node entry may carry an
// `if <condition>` suffix; a false condition drops that entry.
//
// Conditions reference boolean flags (bare identifiers, or the literals
// `true`/`false`) and numeric comparisons (`width < 500`), combined with
// `!`, `&&`, `||` and parentheses. The application supplies their values via Env.
//
// The package imports only layoutspec + stdlib — no Fyne, no device knowledge.
package layoutlang

import (
	"fmt"

	"github.com/mono4loop/rp6/internal/ui/layoutspec"
)

// containerKinds is the set of recognized container type names.
var containerKinds = map[string]bool{
	"VBox": true, "HBox": true, "Stack": true,
	"Border": true, "Split": true, "Grid": true,
	"RackPanel": true,
}

// Document is a parsed layout program: top-level (default-page) variants, named
// application page blocks (each holding its own variants), and named rack blocks
// (per-rack internal arrangements).
type Document struct {
	variants []variant   // top-level variants (the implicit default page)
	pages    []pageBlock // named `page … { … }` blocks, in declaration order
	racks    map[string]*nodeAST
}

// Page is the public metadata for a declared application page: a stable id and a
// display label, in declaration order (see Pages). The page's actual layout is
// the set of `layout` variants inside its `page <id> <Label> { … }` block —
// selected by SelectForPage against the active page.
type Page struct {
	ID    string
	Label string
}

// pageBlock is a named page and the variants declared inside its block. A page
// may be reopened (declared again with the same id) to append more variants; the
// label is taken from the first declaration.
type pageBlock struct {
	id       string
	label    string
	variants []variant
}

// variant is one form-factor arrangement.
type variant struct {
	name string
	when Cond // nil = always matches (the default)
	root *nodeAST
}

// nodeAST is a parsed layout node before conditions are resolved: a widget
// reference, the Spacer keyword, or a container of children/regions/props.
type nodeAST struct {
	ref      string            // non-empty => a widget reference
	refProps map[string]string // properties on a widget reference, e.g. vu(orientation: horizontal)
	kind     string            // container kind, or "Spacer"

	children []childAST           // positional (VBox/HBox/Stack/Grid)
	regions  []regionAST          // named node regions (Border/Split)
	props    map[string]scalarVal // scalar properties (cols/offset/horizontal)
}

type childAST struct {
	n    *nodeAST
	cond Cond
}

type regionAST struct {
	name string
	n    *nodeAST
	cond Cond
}

// scalarVal is a numeric or boolean property value.
type scalarVal struct {
	num    float64
	isNum  bool
	b      bool
	isBool bool
}

// Parse compiles DSL source into a Document. It returns an error with a source
// position on the first lexical or syntactic problem.
func Parse(src string) (*Document, error) {
	toks, err := lex(src)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	return p.parseDocument()
}

// Pages returns the declared application pages in document order. It's empty
// when the document declares none — the application then runs as a single page.
// The returned slice is a copy, safe for the caller to keep.
func (d *Document) Pages() []Page {
	out := make([]Page, len(d.pages))
	for i, p := range d.pages {
		out[i] = Page{ID: p.id, Label: p.label}
	}
	return out
}

// Names returns every variant name in document order: the top-level variants
// first, then each page's variants (handy for logging/tests).
func (d *Document) Names() []string {
	var out []string
	for _, v := range d.variants {
		out = append(out, v.name)
	}
	for _, p := range d.pages {
		for _, v := range p.variants {
			out = append(out, v.name)
		}
	}
	return out
}

// PageRefs returns the widget-reference ids used anywhere in the named page's
// variants (across all form factors, regardless of `if`/`when` conditions), in
// first-seen order. It lets the host tell which racks a page can place — e.g. to
// show a page-appropriate rack menu. Uses variantsForPage, so an empty/unknown id
// resolves to the default/first-page variants.
func (d *Document) PageRefs(pageID string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range d.variantsForPage(pageID) {
		v.root.collectRefs(seen, &out)
	}
	return out
}

// collectRefs walks the node tree, appending each distinct widget-reference id to
// out (first-seen order), ignoring conditions.
func (n *nodeAST) collectRefs(seen map[string]bool, out *[]string) {
	if n == nil {
		return
	}
	if n.ref != "" {
		if !seen[n.ref] {
			seen[n.ref] = true
			*out = append(*out, n.ref)
		}
		return
	}
	for _, c := range n.children {
		c.n.collectRefs(seen, out)
	}
	for _, r := range n.regions {
		r.n.collectRefs(seen, out)
	}
}

// variantsForPage returns the variants to select among for a page id: the named
// page's own variants, or — when pageID is empty or unknown — the top-level
// variants, falling back to the first page's variants when there are no top-level
// ones (so a pages-only document still resolves without an explicit page).
func (d *Document) variantsForPage(pageID string) []variant {
	if pageID != "" {
		for _, p := range d.pages {
			if p.id == pageID {
				return p.variants
			}
		}
	}
	if len(d.variants) > 0 {
		return d.variants
	}
	if len(d.pages) > 0 {
		return d.pages[0].variants
	}
	return nil
}

// SelectForPage evaluates each of the page's variants' `when` against env and
// compiles the first match to a layoutspec.Node, applying every inline `if`
// condition. Returns nil if no variant matches. See variantsForPage for how the
// page id resolves (empty = the default/top-level variants).
func (d *Document) SelectForPage(pageID string, env Env) layoutspec.Node {
	for _, v := range d.variantsForPage(pageID) {
		if v.when == nil || v.when.eval(env) {
			return v.root.resolve(env)
		}
	}
	return nil
}

// SelectedNameForPage returns the name of the variant SelectForPage would choose
// for the page + env, and whether any matched. Useful for diagnostics and tests.
func (d *Document) SelectedNameForPage(pageID string, env Env) (string, bool) {
	for _, v := range d.variantsForPage(pageID) {
		if v.when == nil || v.when.eval(env) {
			return v.name, true
		}
	}
	return "", false
}

// Select is SelectForPage on the default (top-level) variants — the single-page
// path for documents that declare no page blocks.
func (d *Document) Select(env Env) layoutspec.Node { return d.SelectForPage("", env) }

// SelectedName returns the name of the variant Select would choose for env, and
// whether any variant matched. Useful for diagnostics and tests.
func (d *Document) SelectedName(env Env) (string, bool) { return d.SelectedNameForPage("", env) }

// Rack compiles the named `rack` block (a per-rack internal arrangement) to a
// layoutspec.Node, applying inline `if` conditions against env. It returns nil
// if the document has no such block — the caller then keeps its own (Go-built)
// composition, so rack blocks are optional overrides.
func (d *Document) Rack(name string, env Env) layoutspec.Node {
	n, ok := d.racks[name]
	if !ok {
		return nil
	}
	return n.resolve(env)
}

// RackNames returns the names of the defined rack blocks (unordered).
func (d *Document) RackNames() []string {
	out := make([]string, 0, len(d.racks))
	for name := range d.racks {
		out = append(out, name)
	}
	return out
}

// resolve converts a parsed node to a layoutspec.Node, dropping child entries
// and regions whose condition is false under env.
func (n *nodeAST) resolve(env Env) layoutspec.Node {
	if n == nil {
		return nil
	}
	if n.ref != "" {
		if len(n.refProps) > 0 {
			return layoutspec.RefWith(n.ref, n.refProps)
		}
		return layoutspec.Ref(n.ref)
	}
	if n.kind == "Spacer" {
		return layoutspec.Spacer()
	}
	if n.kind == "Separator" {
		return layoutspec.Separator()
	}

	kids := func() []layoutspec.Node {
		var out []layoutspec.Node
		for _, c := range n.children {
			if c.cond != nil && !c.cond.eval(env) {
				continue
			}
			out = append(out, c.n.resolve(env))
		}
		return out
	}

	switch n.kind {
	case "VBox":
		return layoutspec.VBox(kids()...)
	case "HBox":
		return layoutspec.HBox(kids()...)
	case "Stack":
		return layoutspec.Stack(kids()...)
	case "RackPanel":
		return layoutspec.RackPanel(kids()...)
	case "Grid":
		g := layoutspec.Grid{Cols: 1, Children: kids()}
		if p, ok := n.props["cols"]; ok && p.isNum {
			g.Cols = int(p.num)
		}
		return g
	case "Border":
		var b layoutspec.Border
		for _, r := range n.regions {
			if r.cond != nil && !r.cond.eval(env) {
				continue
			}
			switch r.name {
			case "top":
				b.Top = r.n.resolve(env)
			case "bottom":
				b.Bottom = r.n.resolve(env)
			case "left":
				b.Left = r.n.resolve(env)
			case "right":
				b.Right = r.n.resolve(env)
			case "center":
				b.Center = append(b.Center, r.n.resolve(env))
			}
		}
		return b
	case "Split":
		var s layoutspec.Split
		if p, ok := n.props["horizontal"]; ok && p.isBool {
			s.Horizontal = p.b
		}
		if p, ok := n.props["offset"]; ok && p.isNum {
			s.Offset = p.num
		}
		for _, r := range n.regions {
			if r.cond != nil && !r.cond.eval(env) {
				continue
			}
			switch r.name {
			case "leading":
				s.Leading = r.n.resolve(env)
			case "trailing":
				s.Trailing = r.n.resolve(env)
			}
		}
		return s
	}
	return nil
}

// parser is a hand-written recursive-descent parser over the token slice.
type parser struct {
	toks []token
	pos  int
}

func (p *parser) peek() token { return p.toks[p.pos] }

func (p *parser) peekAt(n int) token {
	i := p.pos + n
	if i >= len(p.toks) {
		return p.toks[len(p.toks)-1] // tEOF
	}
	return p.toks[i]
}

func (p *parser) advance() token {
	t := p.toks[p.pos]
	if p.pos < len(p.toks)-1 {
		p.pos++
	}
	return t
}

func (p *parser) expect(k tokKind) (token, error) {
	t := p.peek()
	if t.kind != k {
		return t, p.errf(t, "expected %s, got %s", tokName(k), t)
	}
	return p.advance(), nil
}

// parseError is a syntax error with a source position.
type parseError struct {
	line, col int
	msg       string
}

func (e *parseError) Error() string { return fmt.Sprintf("line %d:%d: %s", e.line, e.col, e.msg) }

func (p *parser) errf(t token, format string, args ...any) error {
	return &parseError{t.line, t.col, fmt.Sprintf(format, args...)}
}

func (p *parser) parseDocument() (*Document, error) {
	d := &Document{racks: map[string]*nodeAST{}}
	for p.peek().kind != tEOF {
		kw := p.peek()
		if kw.kind != tIdent {
			return nil, p.errf(kw, "expected `layout`, `rack` or `page`, got %s", kw)
		}
		switch kw.text {
		case "layout":
			v, err := p.parseVariant()
			if err != nil {
				return nil, err
			}
			d.variants = append(d.variants, v)
		case "rack":
			name, node, err := p.parseRack()
			if err != nil {
				return nil, err
			}
			d.racks[name] = node
		case "page":
			pg, err := p.parsePage()
			if err != nil {
				return nil, err
			}
			d.addPage(pg)
		default:
			return nil, p.errf(kw, "expected `layout`, `rack` or `page`, got %s", kw)
		}
	}
	if len(d.Names()) == 0 {
		return nil, &parseError{1, 1, "empty layout document: expected at least one `layout` block"}
	}
	return d, nil
}

// addPage records a parsed page block, merging into an existing page of the same
// id (reopening: variants are appended, the first label wins).
func (d *Document) addPage(pg pageBlock) {
	for i := range d.pages {
		if d.pages[i].id == pg.id {
			d.pages[i].variants = append(d.pages[i].variants, pg.variants...)
			if d.pages[i].label == "" {
				d.pages[i].label = pg.label
			}
			return
		}
	}
	d.pages = append(d.pages, pg)
}

// parseRack parses a `rack NAME { node }` block: a per-rack internal
// arrangement. Unlike a layout variant it has no `when` guard (a rack has one
// internal composition).
func (p *parser) parseRack() (string, *nodeAST, error) {
	p.advance() // `rack`
	name, err := p.expect(tIdent)
	if err != nil {
		return "", nil, err
	}
	if _, err := p.expect(tLBrace); err != nil {
		return "", nil, err
	}
	node, err := p.parseNode()
	if err != nil {
		return "", nil, err
	}
	p.optSemi()
	if _, err := p.expect(tRBrace); err != nil {
		return "", nil, err
	}
	return name.text, node, nil
}

// parsePage parses a `page <id> <Label> { <layout variants…> }` block: a named
// application page (a stable id + display label) that holds the `layout` variants
// making up its per-form-factor arrangements. The brace body is optional — a bare
// `page <id> <Label>` just declares the page (no variants) — and a page may be
// reopened with the same id to append more variants (see addPage). A page block
// contains only `layout` variants; rack blocks stay top-level (shared).
func (p *parser) parsePage() (pageBlock, error) {
	p.advance() // `page`
	id, err := p.expect(tIdent)
	if err != nil {
		return pageBlock{}, err
	}
	label, err := p.expect(tIdent)
	if err != nil {
		return pageBlock{}, err
	}
	pg := pageBlock{id: id.text, label: label.text}
	if p.peek().kind != tLBrace {
		p.optSemi()
		return pg, nil
	}
	p.advance() // `{`
	for p.peek().kind != tRBrace {
		if p.peek().kind == tEOF {
			return pageBlock{}, p.errf(p.peek(), "unexpected end of input inside page %q (missing `}`?)", id.text)
		}
		if p.peek().kind != tIdent || p.peek().text != "layout" {
			return pageBlock{}, p.errf(p.peek(), "a page block contains only `layout` variants, got %s", p.peek())
		}
		v, err := p.parseVariant()
		if err != nil {
			return pageBlock{}, err
		}
		pg.variants = append(pg.variants, v)
	}
	p.advance() // `}`
	return pg, nil
}

func (p *parser) parseVariant() (variant, error) {
	kw, err := p.expect(tIdent)
	if err != nil {
		return variant{}, err
	}
	if kw.text != "layout" {
		return variant{}, p.errf(kw, "expected `layout`, got %s", kw)
	}
	name, err := p.expect(tIdent)
	if err != nil {
		return variant{}, err
	}
	v := variant{name: name.text}

	if p.peek().kind == tIdent && p.peek().text == "when" {
		p.advance()
		cond, err := p.parseCond()
		if err != nil {
			return variant{}, err
		}
		v.when = cond
	}

	if _, err := p.expect(tLBrace); err != nil {
		return variant{}, err
	}
	root, err := p.parseNode()
	if err != nil {
		return variant{}, err
	}
	v.root = root
	p.optSemi()
	if _, err := p.expect(tRBrace); err != nil {
		return variant{}, err
	}
	return v, nil
}

// parseNode parses a single node: a container (Type { … }), the Spacer/Separator
// keywords, or a widget reference (any other bare identifier), optionally with
// properties: `vu(orientation: horizontal)`.
func (p *parser) parseNode() (*nodeAST, error) {
	id, err := p.expect(tIdent)
	if err != nil {
		return nil, err
	}
	if id.text == "Spacer" {
		return &nodeAST{kind: "Spacer"}, nil
	}
	if id.text == "Separator" {
		return &nodeAST{kind: "Separator"}, nil
	}
	if p.peek().kind == tLBrace {
		if !containerKinds[id.text] {
			return nil, p.errf(id, "unknown container type %q", id.text)
		}
		return p.parseContainer(id.text)
	}
	if isKeyword(id.text) {
		return nil, p.errf(id, "unexpected keyword %q where a widget id was expected", id.text)
	}
	// A widget reference, optionally with properties: name(key: value, …).
	if p.peek().kind == tLParen {
		props, err := p.parseRefProps()
		if err != nil {
			return nil, err
		}
		return &nodeAST{ref: id.text, refProps: props}, nil
	}
	return &nodeAST{ref: id.text}, nil
}

// parseRefProps parses a `(key: value, …)` property list on a widget reference.
// Values are numbers, booleans, or bare identifiers, all kept as strings for the
// application's Configurator to interpret.
func (p *parser) parseRefProps() (map[string]string, error) {
	p.advance() // '('
	props := map[string]string{}
	for p.peek().kind != tRParen {
		if p.peek().kind == tEOF {
			return nil, p.errf(p.peek(), "unexpected end of input in property list (missing `)`?)")
		}
		name, err := p.expect(tIdent)
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(tColon); err != nil {
			return nil, err
		}
		val := p.peek()
		if val.kind != tIdent && val.kind != tNumber {
			return nil, p.errf(val, "expected a property value, got %s", val)
		}
		p.advance()
		props[name.text] = val.text
		if p.peek().kind == tComma {
			p.advance()
		}
	}
	p.advance() // ')'
	return props, nil
}

func (p *parser) parseContainer(kind string) (*nodeAST, error) {
	if _, err := p.expect(tLBrace); err != nil {
		return nil, err
	}
	n := &nodeAST{kind: kind}
	for p.peek().kind != tRBrace {
		if p.peek().kind == tEOF {
			return nil, p.errf(p.peek(), "unexpected end of input inside %s (missing `}`?)", kind)
		}
		if err := p.parseEntry(n); err != nil {
			return nil, err
		}
	}
	p.advance() // consume }
	return n, nil
}

// parseEntry parses one `;`-terminated entry inside a container: a named entry
// `name: value` (a region node or a scalar property) or a positional child node,
// either optionally followed by `if <condition>`.
func (p *parser) parseEntry(n *nodeAST) error {
	// Named entry: IDENT ':' …
	if p.peek().kind == tIdent && p.peekAt(1).kind == tColon {
		name := p.advance()
		p.advance() // ':'
		return p.parseNamedValue(n, name)
	}

	// Positional child node.
	child, err := p.parseNode()
	if err != nil {
		return err
	}
	cond, err := p.parseOptCond()
	if err != nil {
		return err
	}
	if err := requirePositional(p, n, name(child)); err != nil {
		return err
	}
	n.children = append(n.children, childAST{n: child, cond: cond})
	p.optSemi()
	return nil
}

func (p *parser) parseNamedValue(n *nodeAST, nameTok token) error {
	// Scalar property (number or bool literal)?
	switch t := p.peek(); {
	case t.kind == tNumber:
		p.advance()
		if err := p.setProp(n, nameTok, scalarVal{num: parseNumber(t.text), isNum: true}); err != nil {
			return err
		}
		return p.semiNoCond()
	case t.kind == tIdent && (t.text == "true" || t.text == "false"):
		p.advance()
		if err := p.setProp(n, nameTok, scalarVal{b: t.text == "true", isBool: true}); err != nil {
			return err
		}
		return p.semiNoCond()
	}

	// Otherwise a region node value.
	node, err := p.parseNode()
	if err != nil {
		return err
	}
	cond, err := p.parseOptCond()
	if err != nil {
		return err
	}
	if !regionAllowed(n.kind, nameTok.text) {
		return p.errf(nameTok, "%s has no region %q", n.kind, nameTok.text)
	}
	n.regions = append(n.regions, regionAST{name: nameTok.text, n: node, cond: cond})
	p.optSemi()
	return nil
}

func (p *parser) setProp(n *nodeAST, nameTok token, v scalarVal) error {
	if !propAllowed(n.kind, nameTok.text) {
		return p.errf(nameTok, "%s has no property %q", n.kind, nameTok.text)
	}
	if n.props == nil {
		n.props = map[string]scalarVal{}
	}
	n.props[nameTok.text] = v
	return nil
}

// parseOptCond parses an optional `if <condition>` suffix, returning nil if
// absent.
func (p *parser) parseOptCond() (Cond, error) {
	if p.peek().kind == tIdent && p.peek().text == "if" {
		p.advance()
		return p.parseCond()
	}
	return nil, nil
}

// optSemi consumes a `;` entry terminator if present. Terminators are optional:
// container blocks are already delimited by their braces, so a trailing `;` is
// allowed (and idiomatic after refs/props) but never required.
func (p *parser) optSemi() {
	if p.peek().kind == tSemi {
		p.advance()
	}
}

// semiNoCond rejects an `if` on a scalar property, then consumes an optional `;`.
func (p *parser) semiNoCond() error {
	if p.peek().kind == tIdent && p.peek().text == "if" {
		return p.errf(p.peek(), "`if` is not allowed on a scalar property")
	}
	p.optSemi()
	return nil
}

// requirePositional rejects a positional child in a container that only takes
// named regions (Border/Split).
func requirePositional(p *parser, n *nodeAST, childName string) error {
	switch n.kind {
	case "Border", "Split":
		return p.errf(p.peek(), "%s takes named regions, not a positional child (%s)", n.kind, childName)
	}
	return nil
}

func regionAllowed(kind, name string) bool {
	switch kind {
	case "Border":
		switch name {
		case "top", "bottom", "left", "right", "center":
			return true
		}
	case "Split":
		switch name {
		case "leading", "trailing":
			return true
		}
	}
	return false
}

func propAllowed(kind, name string) bool {
	switch kind {
	case "Grid":
		return name == "cols"
	case "Split":
		return name == "horizontal" || name == "offset"
	}
	return false
}

// name returns a short description of a node for error messages.
func name(n *nodeAST) string {
	switch {
	case n == nil:
		return "?"
	case n.ref != "":
		return n.ref
	default:
		return n.kind
	}
}

// tokName gives a human name for a token kind in error messages.
func tokName(k tokKind) string {
	switch k {
	case tIdent:
		return "identifier"
	case tNumber:
		return "number"
	case tLBrace:
		return "`{`"
	case tRBrace:
		return "`}`"
	case tSemi:
		return "`;`"
	case tColon:
		return "`:`"
	case tLParen:
		return "`(`"
	case tRParen:
		return "`)`"
	default:
		return "token"
	}
}
