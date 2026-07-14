package layoutlang

import (
	"fmt"
	"strings"
	"unicode"
)

// tokKind enumerates the lexical token kinds of the layout DSL.
type tokKind int

const (
	tEOF tokKind = iota
	tIdent
	tNumber
	tLBrace // {
	tRBrace // }
	tSemi   // ;
	tColon  // :
	tComma  // ,
	tLParen // (
	tRParen // )
	tNot    // !
	tLt     // <
	tGt     // >
	tLe     // <=
	tGe     // >=
	tEqEq   // ==
	tNe     // !=
	tAnd    // &&
	tOr     // ||
)

// token is a single lexical unit with its source position (for error messages).
type token struct {
	kind tokKind
	text string
	line int
	col  int
}

func (t token) String() string {
	if t.kind == tEOF {
		return "end of input"
	}
	return fmt.Sprintf("%q", t.text)
}

// lexError is a lexical error carrying a source position.
type lexError struct {
	line, col int
	msg       string
}

func (e *lexError) Error() string { return fmt.Sprintf("line %d:%d: %s", e.line, e.col, e.msg) }

// lex tokenizes src into a slice of tokens terminated by a tEOF token. It
// understands // line comments and /* block */ comments, and the two-character
// operators (<=, >=, ==, !=, &&, ||). Identifiers may contain letters, digits,
// underscores and dashes; numbers are decimal with an optional fractional part.
func lex(src string) ([]token, error) {
	var toks []token
	line, col := 1, 1
	i := 0
	n := len(src)

	// advance consumes one byte, tracking line/column.
	advance := func() byte {
		c := src[i]
		i++
		if c == '\n' {
			line++
			col = 1
		} else {
			col++
		}
		return c
	}

	for i < n {
		c := src[i]

		// Whitespace.
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
			advance()
			continue
		}

		// Comments.
		if c == '/' && i+1 < n && src[i+1] == '/' {
			for i < n && src[i] != '\n' {
				advance()
			}
			continue
		}
		if c == '/' && i+1 < n && src[i+1] == '*' {
			startLine, startCol := line, col
			advance()
			advance()
			closed := false
			for i < n {
				if src[i] == '*' && i+1 < n && src[i+1] == '/' {
					advance()
					advance()
					closed = true
					break
				}
				advance()
			}
			if !closed {
				return nil, &lexError{startLine, startCol, "unterminated block comment"}
			}
			continue
		}

		startLine, startCol := line, col

		// Identifiers: [A-Za-z_][A-Za-z0-9_-]*
		if isIdentStart(rune(c)) {
			start := i
			advance()
			for i < n && isIdentPart(rune(src[i])) {
				advance()
			}
			toks = append(toks, token{tIdent, src[start:i], startLine, startCol})
			continue
		}

		// Numbers: [0-9]+(.[0-9]+)?
		if c >= '0' && c <= '9' {
			start := i
			advance()
			for i < n && src[i] >= '0' && src[i] <= '9' {
				advance()
			}
			if i < n && src[i] == '.' {
				advance()
				for i < n && src[i] >= '0' && src[i] <= '9' {
					advance()
				}
			}
			toks = append(toks, token{tNumber, src[start:i], startLine, startCol})
			continue
		}

		// Two- and one-character punctuation/operators.
		two := ""
		if i+1 < n {
			two = src[i : i+2]
		}
		switch two {
		case "<=":
			advance()
			advance()
			toks = append(toks, token{tLe, two, startLine, startCol})
			continue
		case ">=":
			advance()
			advance()
			toks = append(toks, token{tGe, two, startLine, startCol})
			continue
		case "==":
			advance()
			advance()
			toks = append(toks, token{tEqEq, two, startLine, startCol})
			continue
		case "!=":
			advance()
			advance()
			toks = append(toks, token{tNe, two, startLine, startCol})
			continue
		case "&&":
			advance()
			advance()
			toks = append(toks, token{tAnd, two, startLine, startCol})
			continue
		case "||":
			advance()
			advance()
			toks = append(toks, token{tOr, two, startLine, startCol})
			continue
		}

		advance()
		switch c {
		case '{':
			toks = append(toks, token{tLBrace, "{", startLine, startCol})
		case '}':
			toks = append(toks, token{tRBrace, "}", startLine, startCol})
		case ';':
			toks = append(toks, token{tSemi, ";", startLine, startCol})
		case ':':
			toks = append(toks, token{tColon, ":", startLine, startCol})
		case ',':
			toks = append(toks, token{tComma, ",", startLine, startCol})
		case '(':
			toks = append(toks, token{tLParen, "(", startLine, startCol})
		case ')':
			toks = append(toks, token{tRParen, ")", startLine, startCol})
		case '!':
			toks = append(toks, token{tNot, "!", startLine, startCol})
		case '<':
			toks = append(toks, token{tLt, "<", startLine, startCol})
		case '>':
			toks = append(toks, token{tGt, ">", startLine, startCol})
		default:
			return nil, &lexError{startLine, startCol, fmt.Sprintf("unexpected character %q", string(c))}
		}
	}

	toks = append(toks, token{tEOF, "", line, col})
	return toks, nil
}

func isIdentStart(r rune) bool {
	return r == '_' || unicode.IsLetter(r)
}

func isIdentPart(r rune) bool {
	return r == '_' || r == '-' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// isKeyword reports whether ident is a DSL keyword (used to reject keywords as
// widget IDs where that would be ambiguous).
func isKeyword(ident string) bool {
	switch strings.ToLower(ident) {
	case "layout", "when", "if", "true", "false", "page":
		return true
	}
	return false
}
