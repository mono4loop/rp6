// Package mapped is a data-driven midiin controller: it reads a small text
// "MIDI map" (.midimap) that binds a controller's incoming MIDI messages to
// named RP6 control intents, so supporting new hardware is a file, not Go code.
//
// It is the generic driver behind most RP6 controllers: one mapped.Device
// serves any controller whose behavior a .midimap file describes (the Adafruit
// MacroPad, Arturia keyboards, the Synido TempoPAD C16, …). Like the rest of internal/midiin it holds NO RP6 vocabulary —
// it forwards abstract midiin.Intent values whose names the application (cmd/rp6)
// owns and validates. See docs/architecture/midimaps.md for the full design.
package mapped

import (
	"fmt"
	"strconv"
	"strings"
)

// Map is a parsed .midimap: one controller, its discovery substrings, an
// optional default channel filter, and its message→intent bindings.
type Map struct {
	Name     string   // display name, e.g. "Synido TempoPAD C16"
	Match    []string // /proc card / bridge port name substrings (case-folded)
	Channel  int      // default channel filter (1..16), or 0 for "any"
	Bindings []Binding
}

// srcKind is the type of MIDI message a binding matches.
type srcKind int

const (
	srcNote     srcKind = iota // note or note range
	srcCC                      // control change
	srcPC                      // program change
	srcRealtime                // system realtime (start/stop/continue)
	srcMMC                     // MIDI Machine Control (not yet decoded; see §8)
)

// source is the matched MIDI message of a binding.
type source struct {
	kind srcKind
	num  uint8  // note / cc number (low end for a note range)
	hi   uint8  // note range high (== num for a single note)
	word string // realtime/mmc keyword (start/stop/continue/play/record)
}

// whenOp is a value comparison on a message's data byte.
type whenOp int

const (
	whenNone whenOp = iota
	whenEq          // value == v
	whenGE          // value >= v
)

// Binding is one message→intent rule.
type Binding struct {
	Src    source
	Intent string // target intent name (app vocabulary or interpreter-internal)
	Arg    string // optional trailing string argument

	Channel int // per-binding channel override (1..16), or 0 to inherit Map.Channel

	When    whenOp
	WhenVal uint8

	Abs       bool   // absolute value → value shape
	ScaleLo   uint8  // absolute sub-range (default 0..127)
	ScaleHi   uint8  //
	Rel       string // relative encoder encoding (encoder.go), "" if none
	Offset    int    // note→pad id base for pad.trigger[.rel]
	hasOffset bool

	line int // source line, for diagnostics
}

// Parse reads a .midimap document. It returns a descriptive error (with a line
// number) on the first malformed line.
func Parse(text string) (*Map, error) {
	lines := splitLines(stripComments(text))
	m := &Map{}
	inDevice := false
	sawDevice := false
	for _, ln := range lines {
		raw := strings.TrimSpace(ln.text)
		if raw == "" {
			continue
		}
		if !inDevice {
			name, err := parseDeviceHeader(raw, ln.n)
			if err != nil {
				return nil, err
			}
			m.Name = name
			inDevice, sawDevice = true, true
			continue
		}
		if raw == "}" {
			inDevice = false
			continue
		}
		if err := parseStatement(m, raw, ln.n); err != nil {
			return nil, err
		}
	}
	if !sawDevice {
		return nil, fmt.Errorf("midimap: no device block")
	}
	if inDevice {
		return nil, fmt.Errorf("midimap: unterminated device block (missing '}')")
	}
	if m.Name == "" {
		return nil, fmt.Errorf("midimap: device has no name")
	}
	if len(m.Match) == 0 {
		return nil, fmt.Errorf("midimap: device %q has no match strings", m.Name)
	}
	return m, nil
}

// parseDeviceHeader parses `device "Name" {`.
func parseDeviceHeader(s string, line int) (string, error) {
	rest, ok := strings.CutPrefix(s, "device")
	if !ok || (len(s) > 6 && !isSpace(s[6])) {
		return "", fmt.Errorf("midimap:%d: expected `device \"name\" {`, got %q", line, s)
	}
	name, tail, err := cutQuoted(strings.TrimSpace(rest))
	if err != nil {
		return "", fmt.Errorf("midimap:%d: %v", line, err)
	}
	if strings.TrimSpace(tail) != "{" {
		return "", fmt.Errorf("midimap:%d: expected `{` after device name", line)
	}
	return name, nil
}

// parseStatement parses one line inside the device block: match, channel, or a
// binding.
func parseStatement(m *Map, s string, line int) error {
	head, _, _ := strings.Cut(s, " ")
	switch head {
	case "match":
		subs := extractQuoted(strings.TrimPrefix(s, "match"))
		if len(subs) == 0 {
			return fmt.Errorf("midimap:%d: match needs at least one \"substring\"", line)
		}
		for _, x := range subs {
			m.Match = append(m.Match, strings.ToLower(x))
		}
		return nil
	case "channel":
		n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(s, "channel")))
		if err != nil || n < 1 || n > 16 {
			return fmt.Errorf("midimap:%d: channel must be 1..16", line)
		}
		m.Channel = n
		return nil
	default:
		b, err := parseBinding(s, line)
		if err != nil {
			return err
		}
		m.Bindings = append(m.Bindings, b)
		return nil
	}
}

// parseBinding parses `<source> [modifiers…] -> <intent> [modifiers…|arg]`.
// Modifiers (abs/rel=/scale=/offset=/when/on) may appear on either side of `->`;
// the token right after `->` is always the intent, and one bare non-modifier
// token there is its optional arg.
func parseBinding(s string, line int) (Binding, error) {
	lhs, rhs, ok := strings.Cut(s, "->")
	if !ok {
		return Binding{}, fmt.Errorf("midimap:%d: binding needs `->`: %q", line, s)
	}
	ltoks := strings.Fields(lhs)
	rtoks := strings.Fields(rhs)
	if len(ltoks) == 0 {
		return Binding{}, fmt.Errorf("midimap:%d: empty binding source", line)
	}
	if len(rtoks) == 0 {
		return Binding{}, fmt.Errorf("midimap:%d: binding has no intent", line)
	}
	b := Binding{ScaleLo: 0, ScaleHi: 127, line: line}

	// Source (consumes 1..2 leading LHS tokens).
	used, err := parseSource(&b, ltoks, line)
	if err != nil {
		return Binding{}, err
	}
	b.Intent = rtoks[0]

	// Modifiers may appear after the source (LHS) or after the intent (RHS).
	// A bare non-modifier token is only allowed on the RHS, where it's the arg.
	if err := applyMods(&b, ltoks[used:], false, line); err != nil {
		return Binding{}, err
	}
	if err := applyMods(&b, rtoks[1:], true, line); err != nil {
		return Binding{}, err
	}
	if b.Abs && b.Rel != "" {
		return Binding{}, fmt.Errorf("midimap:%d: `abs` and `rel=` are mutually exclusive", line)
	}
	return b, nil
}

// applyMods parses modifier tokens into b. When allowArg is set, a single bare
// (no `=`) non-keyword token is taken as the intent's arg; otherwise a bare
// token is an error.
func applyMods(b *Binding, toks []string, allowArg bool, line int) error {
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		switch {
		case t == "on":
			i++
			if i >= len(toks) {
				return fmt.Errorf("midimap:%d: `on` needs a channel", line)
			}
			ch, err := strconv.Atoi(toks[i])
			if err != nil || ch < 1 || ch > 16 {
				return fmt.Errorf("midimap:%d: `on` channel must be 1..16", line)
			}
			b.Channel = ch
		case t == "when":
			i++
			if i >= len(toks) {
				return fmt.Errorf("midimap:%d: `when` needs value=<n> or value>=<n>", line)
			}
			if err := parseWhen(b, toks[i], line); err != nil {
				return err
			}
		case t == "abs":
			b.Abs = true
		case strings.HasPrefix(t, "scale="):
			lo, hi, err := parseRange(strings.TrimPrefix(t, "scale="), line)
			if err != nil {
				return err
			}
			b.ScaleLo, b.ScaleHi = lo, hi
		case strings.HasPrefix(t, "rel="):
			b.Rel = strings.TrimPrefix(t, "rel=")
			if !validEncoding(b.Rel) {
				return fmt.Errorf("midimap:%d: unknown rel encoding %q", line, b.Rel)
			}
		case strings.HasPrefix(t, "offset="):
			n, err := strconv.Atoi(strings.TrimPrefix(t, "offset="))
			if err != nil {
				return fmt.Errorf("midimap:%d: bad offset", line)
			}
			b.Offset, b.hasOffset = n, true
		case allowArg && !strings.ContainsRune(t, '='):
			if b.Arg != "" {
				return fmt.Errorf("midimap:%d: unexpected token %q", line, t)
			}
			b.Arg = t
		default:
			return fmt.Errorf("midimap:%d: unknown modifier %q", line, t)
		}
	}
	return nil
}

// parseSource fills b.Src from the leading tokens and returns how many it used.
func parseSource(b *Binding, toks []string, line int) (int, error) {
	switch toks[0] {
	case "note":
		if len(toks) < 2 {
			return 0, fmt.Errorf("midimap:%d: note needs a number or range", line)
		}
		lo, hi, err := parseNoteOperand(toks[1], line)
		if err != nil {
			return 0, err
		}
		b.Src = source{kind: srcNote, num: lo, hi: hi}
		return 2, nil
	case "cc":
		if len(toks) < 2 {
			return 0, fmt.Errorf("midimap:%d: cc needs a number", line)
		}
		n, err := parseByte(toks[1], line)
		if err != nil {
			return 0, err
		}
		b.Src = source{kind: srcCC, num: n, hi: n}
		return 2, nil
	case "pc":
		b.Src = source{kind: srcPC}
		return 1, nil
	case "realtime":
		if len(toks) < 2 {
			return 0, fmt.Errorf("midimap:%d: realtime needs start|stop|continue", line)
		}
		b.Src = source{kind: srcRealtime, word: toks[1]}
		return 2, nil
	case "mmc":
		if len(toks) < 2 {
			return 0, fmt.Errorf("midimap:%d: mmc needs a command", line)
		}
		b.Src = source{kind: srcMMC, word: toks[1]}
		return 2, nil
	default:
		return 0, fmt.Errorf("midimap:%d: unknown source %q", line, toks[0])
	}
}

func parseWhen(b *Binding, tok string, line int) error {
	switch {
	case strings.HasPrefix(tok, "value>="):
		v, err := parseByte(strings.TrimPrefix(tok, "value>="), line)
		if err != nil {
			return err
		}
		b.When, b.WhenVal = whenGE, v
	case strings.HasPrefix(tok, "value="):
		v, err := parseByte(strings.TrimPrefix(tok, "value="), line)
		if err != nil {
			return err
		}
		b.When, b.WhenVal = whenEq, v
	default:
		return fmt.Errorf("midimap:%d: bad `when` clause %q", line, tok)
	}
	return nil
}

func parseNoteOperand(tok string, line int) (lo, hi uint8, err error) {
	if strings.Contains(tok, "..") {
		return parseRange(tok, line)
	}
	n, err := parseByte(tok, line)
	return n, n, err
}

func parseRange(tok string, line int) (lo, hi uint8, err error) {
	a, b, ok := strings.Cut(tok, "..")
	if !ok {
		return 0, 0, fmt.Errorf("midimap:%d: expected lo..hi, got %q", line, tok)
	}
	if lo, err = parseByte(a, line); err != nil {
		return 0, 0, err
	}
	if hi, err = parseByte(b, line); err != nil {
		return 0, 0, err
	}
	if lo > hi {
		return 0, 0, fmt.Errorf("midimap:%d: range %q is inverted", line, tok)
	}
	return lo, hi, nil
}

func parseByte(tok string, line int) (uint8, error) {
	n, err := strconv.Atoi(strings.TrimSpace(tok))
	if err != nil || n < 0 || n > 127 {
		return 0, fmt.Errorf("midimap:%d: %q is not a 0..127 value", line, tok)
	}
	return uint8(n), nil
}

// --- comment stripping & tokenizing helpers ---

type textLine struct {
	n    int
	text string
}

func splitLines(s string) []textLine {
	var out []textLine
	for i, l := range strings.Split(s, "\n") {
		out = append(out, textLine{n: i + 1, text: l})
	}
	return out
}

// stripComments removes // line comments and /* */ block comments while leaving
// content inside double quotes untouched. Newlines are preserved so line
// numbers stay accurate.
func stripComments(s string) string {
	var b strings.Builder
	inStr, inBlock := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inBlock {
			if c == '*' && i+1 < len(s) && s[i+1] == '/' {
				inBlock = false
				i++
			} else if c == '\n' {
				b.WriteByte('\n')
			}
			continue
		}
		if inStr {
			b.WriteByte(c)
			if c == '"' {
				inStr = false
			}
			continue
		}
		switch {
		case c == '"':
			inStr = true
			b.WriteByte(c)
		case c == '/' && i+1 < len(s) && s[i+1] == '/':
			for i < len(s) && s[i] != '\n' {
				i++
			}
			b.WriteByte('\n')
		case c == '/' && i+1 < len(s) && s[i+1] == '*':
			inBlock = true
			i++
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// cutQuoted reads a leading "..." string from s and returns it plus the rest.
func cutQuoted(s string) (val, rest string, err error) {
	if len(s) == 0 || s[0] != '"' {
		return "", "", fmt.Errorf("expected a quoted string")
	}
	end := strings.IndexByte(s[1:], '"')
	if end < 0 {
		return "", "", fmt.Errorf("unterminated quoted string")
	}
	return s[1 : 1+end], s[2+end:], nil
}

// extractQuoted returns every "..." string in s (used for the match list).
func extractQuoted(s string) []string {
	var out []string
	for {
		i := strings.IndexByte(s, '"')
		if i < 0 {
			return out
		}
		j := strings.IndexByte(s[i+1:], '"')
		if j < 0 {
			return out
		}
		out = append(out, s[i+1:i+1+j])
		s = s[i+2+j:]
	}
}

func isSpace(b byte) bool { return b == ' ' || b == '\t' }
