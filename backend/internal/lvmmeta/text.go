// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

package lvmmeta

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// nodeKind distinguishes a config node's shape.
type nodeKind int

const (
	kindScalar nodeKind = iota
	kindSection
	kindArray
)

// Node is one lvm2 config node: name = scalar | { section } | [ array ].
type Node struct {
	Name     string
	Kind     nodeKind
	Value    string   // scalar (integers kept as their decimal text)
	Values   []string // array elements
	Children []*Node  // section children
}

// parseText parses the whole lvm2 config text into a tree. It enforces
// token/depth/string/array limits and fails closed on any syntax error.
func parseText(b []byte) (map[string]*Node, error) {
	if !utf8.Valid(b) {
		return nil, fmt.Errorf("text is not valid UTF-8")
	}
	toks, err := tokenize(b)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks, depth: 0}
	root := map[string]*Node{}
	for p.more() && !p.peekIs(tokRBrace) {
		n, err := p.parseNode()
		if err != nil {
			return nil, err
		}
		root[n.Name] = n
	}
	if p.more() {
		return nil, fmt.Errorf("unexpected trailing token")
	}
	return root, nil
}

type tokKind int

const (
	tokIdent tokKind = iota
	tokInt
	tokString
	tokEq
	tokLBrace
	tokRBrace
	tokLBracket
	tokRBracket
	tokComma
	tokEOF
)

type token struct {
	kind tokKind
	text string
	pos  int
}

// tokenize splits the config text into tokens, dropping comments and
// whitespace, and enforcing the string and token limits. It returns an error
// (never a truncated token slice) when a limit is exceeded or a string is
// unterminated, so a complete-looking prefix cannot be accepted as success.
func tokenize(b []byte) ([]token, error) {
	var toks []token
	i := 0
	for i < len(b) {
		c := b[i]
		switch {
		case c == '#':
			for i < len(b) && b[i] != '\n' {
				i++
			}
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '=':
			toks = append(toks, token{tokEq, "=", i})
			i++
		case c == '{':
			toks = append(toks, token{tokLBrace, "{", i})
			i++
		case c == '}':
			toks = append(toks, token{tokRBrace, "}", i})
			i++
		case c == '[':
			toks = append(toks, token{tokLBracket, "[", i})
			i++
		case c == ']':
			toks = append(toks, token{tokRBracket, "]", i})
			i++
		case c == ',':
			toks = append(toks, token{tokComma, ",", i})
			i++
		case c == '"':
			s, j, ok := scanString(b, i)
			if !ok {
				return nil, fmt.Errorf("string too long or unterminated at offset %d", i)
			}
			toks = append(toks, token{tokString, s, i})
			i = j
		default:
			j := i
			for j < len(b) && isIdentByte(b[j]) {
				j++
			}
			if j == i {
				// Unknown byte: always advance, but fail closed.
				return nil, fmt.Errorf("invalid byte %q at offset %d", c, i)
			}
			word := string(b[i:j])
			if _, err := strconv.ParseInt(word, 10, 64); err == nil {
				toks = append(toks, token{tokInt, word, i})
			} else {
				toks = append(toks, token{tokIdent, word, i})
			}
			i = j
		}
		if len(toks) > MaxTokens {
			return nil, fmt.Errorf("token limit exceeded")
		}
	}
	toks = append(toks, token{tokEOF, "", len(b)})
	return toks, nil
}

func isIdentByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
		c == '_' || c == '-' || c == '.' || c == '/' || c == '+'
}

// scanString reads a double-quoted string with \" and \\ escapes.
func scanString(b []byte, i int) (string, int, bool) {
	// i points at the opening quote.
	var sb strings.Builder
	j := i + 1
	for j < len(b) {
		c := b[j]
		if c == '"' {
			return sb.String(), j + 1, sb.Len() <= MaxString
		}
		if c == '\\' && j+1 < len(b) {
			j++
			switch b[j] {
			case 'n':
				sb.WriteByte('\n')
			case 't':
				sb.WriteByte('\t')
			case 'r':
				sb.WriteByte('\r')
			case '\\', '"':
				sb.WriteByte(b[j])
			default:
				sb.WriteByte(b[j])
			}
			j++
			continue
		}
		sb.WriteByte(c)
		j++
		if sb.Len() > MaxString {
			return "", j, false
		}
	}
	return "", len(b), false // unterminated
}

type parser struct {
	toks  []token
	pos   int
	depth int
}

func (p *parser) more() bool { return p.pos < len(p.toks) && p.toks[p.pos].kind != tokEOF }
func (p *parser) peekIs(k tokKind) bool {
	return p.pos < len(p.toks) && p.toks[p.pos].kind == k
}
func (p *parser) next() token {
	t := p.toks[p.pos]
	if p.pos < len(p.toks)-1 {
		p.pos++
	}
	return t
}

func (p *parser) ident() string {
	// A key may be an identifier or an all-digit name (a bare number in key
	// position is a name, not a value).
	if p.more() && (p.toks[p.pos].kind == tokIdent || p.toks[p.pos].kind == tokInt) {
		return p.next().text
	}
	return ""
}

// parseNode parses one entry: either "name = value" or "name { ... }".
func (p *parser) parseNode() (*Node, error) {
	name := p.ident()
	if name == "" {
		return nil, fmt.Errorf("expected a key at offset %d", p.pos)
	}
	if p.peekIs(tokEq) {
		p.next()
		if p.peekIs(tokLBrace) {
			n, err := p.section()
			if err == nil {
				n.Name = name
			}
			return n, err
		}
		if p.peekIs(tokLBracket) {
			n, err := p.array()
			if err == nil {
				n.Name = name
			}
			return n, err
		}
		if p.peekIs(tokInt) || p.peekIs(tokString) || p.peekIs(tokIdent) {
			n, err := p.scalar()
			if err == nil {
				n.Name = name
			}
			return n, err
		}
		return nil, fmt.Errorf("expected a value at offset %d", p.pos)
	}
	if p.peekIs(tokLBrace) {
		n, err := p.section()
		if err == nil {
			n.Name = name
		}
		return n, err
	}
	return nil, fmt.Errorf("expected '=' or '{' at offset %d", p.pos)
}

func (p *parser) scalar() (*Node, error) {
	t := p.next()
	return &Node{Kind: kindScalar, Value: t.text}, nil
}

func (p *parser) array() (*Node, error) {
	p.next() // [
	n := &Node{Kind: kindArray}
	for p.more() && !p.peekIs(tokRBracket) {
		if p.peekIs(tokComma) {
			p.next()
			continue
		}
		t := p.next()
		if t.kind == tokString || t.kind == tokInt || t.kind == tokIdent {
			n.Values = append(n.Values, t.text)
			if len(n.Values) > MaxArray {
				return nil, fmt.Errorf("array too large at offset %d", t.pos)
			}
		} else {
			return nil, fmt.Errorf("bad array element at offset %d", t.pos)
		}
	}
	if !p.peekIs(tokRBracket) {
		return nil, fmt.Errorf("unterminated array")
	}
	p.next()
	return n, nil
}

func (p *parser) section() (*Node, error) {
	p.next() // {
	p.depth++
	if p.depth > MaxDepth {
		return nil, fmt.Errorf("nesting too deep")
	}
	defer func() { p.depth-- }()
	n := &Node{Kind: kindSection}
	for p.more() && !p.peekIs(tokRBrace) {
		child, err := p.parseNode()
		if err != nil {
			return nil, err
		}
		n.Children = append(n.Children, child)
	}
	if !p.peekIs(tokRBrace) {
		return nil, fmt.Errorf("unterminated section")
	}
	p.next()
	return n, nil
}

// child returns the first child of a section with the given name, or nil.
func (n *Node) child(name string) *Node {
	if n == nil {
		return nil
	}
	for _, c := range n.Children {
		if c.Name == name {
			return c
		}
	}
	return nil
}
