package layoutlang

import "fmt"

// Env supplies the values a layout's conditions are evaluated against: named
// boolean flags (e.g. "compact", "seq_docked") and named numeric variables
// (e.g. "width", "height"). A missing bool reads as false; a missing number
// reads as 0.
type Env struct {
	Bools map[string]bool
	Nums  map[string]float64
}

func (e Env) boolVar(name string) bool {
	if e.Bools == nil {
		return false
	}
	return e.Bools[name]
}

func (e Env) numVar(name string) float64 {
	if e.Nums == nil {
		return 0
	}
	return e.Nums[name]
}

// Cond is a boolean condition parsed from a `when`/`if` clause. It is evaluated
// against an Env at layout-selection time. The interface method is unexported so
// the only way to obtain a Cond is by parsing a layout document.
type Cond interface {
	eval(Env) bool
}

// identCond is a bare boolean flag reference, e.g. `compact`.
type identCond struct{ name string }

func (c identCond) eval(e Env) bool { return e.boolVar(c.name) }

// litCond is a boolean literal (`true` / `false`).
type litCond struct{ v bool }

func (c litCond) eval(Env) bool { return c.v }

// notCond negates its operand (`!x`).
type notCond struct{ c Cond }

func (c notCond) eval(e Env) bool { return !c.c.eval(e) }

// andCond / orCond are short-circuiting boolean combinators.
type andCond struct{ a, b Cond }

func (c andCond) eval(e Env) bool { return c.a.eval(e) && c.b.eval(e) }

type orCond struct{ a, b Cond }

func (c orCond) eval(e Env) bool { return c.a.eval(e) || c.b.eval(e) }

// cmpCond compares a numeric variable against a constant, e.g. `width < 500`.
type cmpCond struct {
	name string
	op   tokKind
	val  float64
}

func (c cmpCond) eval(e Env) bool {
	lhs := e.numVar(c.name)
	switch c.op {
	case tLt:
		return lhs < c.val
	case tGt:
		return lhs > c.val
	case tLe:
		return lhs <= c.val
	case tGe:
		return lhs >= c.val
	case tEqEq:
		return lhs == c.val
	case tNe:
		return lhs != c.val
	}
	return false
}

// parseCond parses a condition expression with the precedence
// (lowest→highest): || , && , ! , comparison/primary.
func (p *parser) parseCond() (Cond, error) { return p.parseOr() }

func (p *parser) parseOr() (Cond, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tOr {
		p.advance()
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = orCond{left, right}
	}
	return left, nil
}

func (p *parser) parseAnd() (Cond, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tAnd {
		p.advance()
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		left = andCond{left, right}
	}
	return left, nil
}

func (p *parser) parseUnary() (Cond, error) {
	if p.peek().kind == tNot {
		p.advance()
		c, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return notCond{c}, nil
	}
	return p.parsePrimary()
}

func (p *parser) parsePrimary() (Cond, error) {
	t := p.peek()
	switch t.kind {
	case tLParen:
		p.advance()
		c, err := p.parseCond()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(tRParen); err != nil {
			return nil, err
		}
		return c, nil
	case tIdent:
		p.advance()
		// A comparison operator makes this a numeric comparison.
		if op := p.peek().kind; op == tLt || op == tGt || op == tLe || op == tGe || op == tEqEq || op == tNe {
			p.advance()
			num, err := p.expect(tNumber)
			if err != nil {
				return nil, err
			}
			return cmpCond{name: t.text, op: op, val: parseNumber(num.text)}, nil
		}
		switch t.text {
		case "true":
			return litCond{true}, nil
		case "false":
			return litCond{false}, nil
		}
		return identCond{name: t.text}, nil
	default:
		return nil, p.errf(t, "expected a condition, got %s", t)
	}
}

func parseNumber(s string) float64 {
	var f float64
	// s is guaranteed a valid decimal by the lexer; ignore the (impossible) err.
	_, _ = fmt.Sscanf(s, "%g", &f)
	return f
}
