// Package expr implements a small expression language used for state
// transition conditions.
//
// Expressions are parsed into an AST and then compiled into immutable
// closures that evaluate against a parameter lookup. The compiled form is
// safe for concurrent use by multiple entities.
//
// Supported operators (in precedence order):
//
//	||
//	&&
//	== !=
//	< > <= >=
//	+ -
//	* /
//	! (unary), - (unary)
//	( )
//
// Literals are floating point numbers; identifiers refer to external
// parameters. Boolean results are represented as 1.0 (true) and 0.0
// (false).
package expr

import (
	"fmt"
	"strconv"
	"strings"
)

// ParamLookup resolves a parameter by name. The second result is false when
// the parameter is not available to the evaluating entity.
type ParamLookup interface {
	GetParam(name string) (float64, bool)
}

// Node is the interface implemented by expression AST nodes.
type Node interface {
	exprNode()
}

// Literal is a constant numeric value.
type Literal struct{ Value float64 }

// ParamRef is a reference to an external parameter.
type ParamRef struct{ Name string }

// Binary is a binary operation. Op is one of + - * / < > <= >= == != && ||.
type Binary struct {
	Op       byte
	Left     Node
	Right    Node
	OpString string
}

// Unary is a unary operation. Op is '!' or '-'.
type Unary struct {
	Op      byte
	Operand Node
}

func (*Literal) exprNode()  {}
func (*ParamRef) exprNode() {}
func (*Binary) exprNode()   {}
func (*Unary) exprNode()    {}

// Parse parses an expression string into an AST.
func Parse(s string) (Node, error) {
	p := &parser{s: s}
	p.next()
	n, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if p.tok != tokEOF {
		return nil, fmt.Errorf("expr: unexpected token %q at offset %d", p.tokLit, p.pos)
	}
	return n, nil
}

// Compile compiles an AST into an immutable evaluation function.
//
// The returned function returns the numeric result and whether evaluation
// succeeded. A missing parameter yields (0, false); callers treat that as a
// non-firing condition.
func Compile(n Node) (func(ParamLookup) (float64, bool), error) {
	switch t := n.(type) {
	case *Literal:
		v := t.Value
		return func(ParamLookup) (float64, bool) { return v, true }, nil
	case *ParamRef:
		name := t.Name
		return func(p ParamLookup) (float64, bool) {
			if p == nil {
				return 0, false
			}
			v, ok := p.GetParam(name)
			if !ok {
				return 0, false
			}
			return v, true
		}, nil
	case *Unary:
		inner, err := Compile(t.Operand)
		if err != nil {
			return nil, err
		}
		switch t.Op {
		case '!':
			return func(p ParamLookup) (float64, bool) {
				v, ok := inner(p)
				if !ok {
					return 0, false
				}
				if v != 0 {
					return 0, true
				}
				return 1, true
			}, nil
		case '-':
			return func(p ParamLookup) (float64, bool) {
				v, ok := inner(p)
				if !ok {
					return 0, false
				}
				return -v, true
			}, nil
		default:
			return nil, fmt.Errorf("expr: unknown unary operator %q", string(t.Op))
		}
	case *Binary:
		lc, err := Compile(t.Left)
		if err != nil {
			return nil, err
		}
		rc, err := Compile(t.Right)
		if err != nil {
			return nil, err
		}
		switch t.Op {
		case '+':
			return func(p ParamLookup) (float64, bool) {
				a, ok := lc(p)
				if !ok {
					return 0, false
				}
				b, ok := rc(p)
				if !ok {
					return 0, false
				}
				return a + b, true
			}, nil
		case '-':
			return func(p ParamLookup) (float64, bool) {
				a, ok := lc(p)
				if !ok {
					return 0, false
				}
				b, ok := rc(p)
				if !ok {
					return 0, false
				}
				return a - b, true
			}, nil
		case '*':
			return func(p ParamLookup) (float64, bool) {
				a, ok := lc(p)
				if !ok {
					return 0, false
				}
				b, ok := rc(p)
				if !ok {
					return 0, false
				}
				return a * b, true
			}, nil
		case '/':
			return func(p ParamLookup) (float64, bool) {
				a, ok := lc(p)
				if !ok {
					return 0, false
				}
				b, ok := rc(p)
				if !ok {
					return 0, false
				}
				if b == 0 {
					return 0, false
				}
				return a / b, true
			}, nil
		case '<':
			return cmp(lc, rc, func(a, b float64) bool { return a < b }), nil
		case '>':
			return cmp(lc, rc, func(a, b float64) bool { return a > b }), nil
		case tokLE:
			return cmp(lc, rc, func(a, b float64) bool { return a <= b }), nil
		case tokGE:
			return cmp(lc, rc, func(a, b float64) bool { return a >= b }), nil
		case tokEQ:
			return cmp(lc, rc, func(a, b float64) bool { return a == b }), nil
		case tokNE:
			return cmp(lc, rc, func(a, b float64) bool { return a != b }), nil
		case tokAND:
			return func(p ParamLookup) (float64, bool) {
				a, ok := lc(p)
				if !ok {
					return 0, false
				}
				if a == 0 {
					return 0, true
				}
				b, ok := rc(p)
				if !ok {
					return 0, false
				}
				if b == 0 {
					return 0, true
				}
				return 1, true
			}, nil
		case tokOR:
			return func(p ParamLookup) (float64, bool) {
				a, ok := lc(p)
				if !ok {
					return 0, false
				}
				if a != 0 {
					return 1, true
				}
				b, ok := rc(p)
				if !ok {
					return 0, false
				}
				if b != 0 {
					return 1, true
				}
				return 0, true
			}, nil
		default:
			return nil, fmt.Errorf("expr: unknown binary operator %q", t.OpString)
		}
	default:
		return nil, fmt.Errorf("expr: unknown node type %T", n)
	}
}

func cmp(lc, rc func(ParamLookup) (float64, bool), f func(float64, float64) bool) func(ParamLookup) (float64, bool) {
	return func(p ParamLookup) (float64, bool) {
		a, ok := lc(p)
		if !ok {
			return 0, false
		}
		b, ok := rc(p)
		if !ok {
			return 0, false
		}
		if f(a, b) {
			return 1, true
		}
		return 0, true
	}
}

// ---------------------------------------------------------------------------
// Tokenizer / parser
// ---------------------------------------------------------------------------

const (
	tokEOF   = 0
	tokNum   = 1
	tokIdent = 2
	tokLE    = 3
	tokGE    = 4
	tokEQ    = 5
	tokNE    = 6
	tokAND   = 7
	tokOR    = 8
)

type parser struct {
	s      string
	pos    int
	tok    int
	tokLit string
}

func (p *parser) next() {
	s := p.s
	for p.pos < len(s) && (s[p.pos] == ' ' || s[p.pos] == '\t' || s[p.pos] == '\n' || s[p.pos] == '\r') {
		p.pos++
	}
	if p.pos >= len(s) {
		p.tok = tokEOF
		p.tokLit = ""
		return
	}
	c := s[p.pos]
	switch {
	case c >= '0' && c <= '9', c == '.':
		start := p.pos
		for p.pos < len(s) && ((s[p.pos] >= '0' && s[p.pos] <= '9') || s[p.pos] == '.') {
			p.pos++
		}
		p.tokLit = s[start:p.pos]
		p.tok = tokNum
	case (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_':
		start := p.pos
		for p.pos < len(s) && ((s[p.pos] >= 'a' && s[p.pos] <= 'z') || (s[p.pos] >= 'A' && s[p.pos] <= 'Z') || (s[p.pos] >= '0' && s[p.pos] <= '9') || s[p.pos] == '_' || s[p.pos] == '.') {
			p.pos++
		}
		p.tokLit = s[start:p.pos]
		p.tok = tokIdent
	default:
		p.pos++
		switch c {
		case '<':
			if p.pos < len(s) && s[p.pos] == '=' {
				p.pos++
				p.tok = tokLE
				p.tokLit = "<="
			} else {
				p.tok = '<'
				p.tokLit = "<"
			}
		case '>':
			if p.pos < len(s) && s[p.pos] == '=' {
				p.pos++
				p.tok = tokGE
				p.tokLit = ">="
			} else {
				p.tok = '>'
				p.tokLit = ">"
			}
		case '=':
			if p.pos < len(s) && s[p.pos] == '=' {
				p.pos++
				p.tok = tokEQ
				p.tokLit = "=="
			} else {
				p.tok = tokEOF
				p.tokLit = "="
			}
		case '!':
			if p.pos < len(s) && s[p.pos] == '=' {
				p.pos++
				p.tok = tokNE
				p.tokLit = "!="
			} else {
				p.tok = '!'
				p.tokLit = "!"
			}
		case '&':
			if p.pos < len(s) && s[p.pos] == '&' {
				p.pos++
				p.tok = tokAND
				p.tokLit = "&&"
			} else {
				p.tok = tokEOF
				p.tokLit = "&"
			}
		case '|':
			if p.pos < len(s) && s[p.pos] == '|' {
				p.pos++
				p.tok = tokOR
				p.tokLit = "||"
			} else {
				p.tok = tokEOF
				p.tokLit = "|"
			}
		case '+', '-', '*', '/', '(', ')':
			p.tok = int(c)
			p.tokLit = string(c)
		default:
			p.tok = tokEOF
			p.tokLit = string(c)
		}
	}
}

func (p *parser) accept(t int) bool {
	if p.tok == t {
		p.next()
		return true
	}
	return false
}

func (p *parser) expect(t int) error {
	if p.tok != t {
		return fmt.Errorf("expr: expected token %d, got %q at offset %d", t, p.tokLit, p.pos)
	}
	p.next()
	return nil
}

func (p *parser) parseOr() (Node, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.tok == tokOR {
		p.next()
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = &Binary{Op: tokOR, Left: left, Right: right, OpString: "||"}
	}
	return left, nil
}

func (p *parser) parseAnd() (Node, error) {
	left, err := p.parseCmp()
	if err != nil {
		return nil, err
	}
	for p.tok == tokAND {
		p.next()
		right, err := p.parseCmp()
		if err != nil {
			return nil, err
		}
		left = &Binary{Op: tokAND, Left: left, Right: right, OpString: "&&"}
	}
	return left, nil
}

func (p *parser) parseCmp() (Node, error) {
	left, err := p.parseAdd()
	if err != nil {
		return nil, err
	}
	switch p.tok {
	case tokEQ, tokNE, '<', '>', tokLE, tokGE:
		op := p.tok
		opStr := p.tokLit
		p.next()
		right, err := p.parseAdd()
		if err != nil {
			return nil, err
		}
		return &Binary{Op: byte(op), Left: left, Right: right, OpString: opStr}, nil
	}
	return left, nil
}

func (p *parser) parseAdd() (Node, error) {
	left, err := p.parseMul()
	if err != nil {
		return nil, err
	}
	for p.tok == '+' || p.tok == '-' {
		op := byte(p.tok)
		p.next()
		right, err := p.parseMul()
		if err != nil {
			return nil, err
		}
		left = &Binary{Op: op, Left: left, Right: right, OpString: string(op)}
	}
	return left, nil
}

func (p *parser) parseMul() (Node, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for p.tok == '*' || p.tok == '/' {
		op := byte(p.tok)
		p.next()
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		left = &Binary{Op: op, Left: left, Right: right, OpString: string(op)}
	}
	return left, nil
}

func (p *parser) parseUnary() (Node, error) {
	if p.tok == '!' || p.tok == '-' {
		op := byte(p.tok)
		p.next()
		operand, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &Unary{Op: op, Operand: operand}, nil
	}
	return p.parsePrimary()
}

func (p *parser) parsePrimary() (Node, error) {
	switch p.tok {
	case tokNum:
		v, err := strconv.ParseFloat(p.tokLit, 64)
		if err != nil {
			return nil, fmt.Errorf("expr: invalid number %q", p.tokLit)
		}
		p.next()
		return &Literal{Value: v}, nil
	case tokIdent:
		name := p.tokLit
		p.next()
		return &ParamRef{Name: name}, nil
	case '(':
		p.next()
		n, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if err := p.expect(')'); err != nil {
			return nil, err
		}
		return n, nil
	default:
		return nil, fmt.Errorf("expr: unexpected token %q at offset %d", p.tokLit, p.pos)
	}
}

// String renders an AST back to a canonical expression string.
func String(n Node) string {
	var b strings.Builder
	writeNode(&b, n)
	return b.String()
}

func writeNode(b *strings.Builder, n Node) {
	switch t := n.(type) {
	case *Literal:
		fmt.Fprintf(b, "%g", t.Value)
	case *ParamRef:
		b.WriteString(t.Name)
	case *Unary:
		b.WriteByte(t.Op)
		writeNode(b, t.Operand)
	case *Binary:
		b.WriteByte('(')
		writeNode(b, t.Left)
		b.WriteByte(' ')
		b.WriteString(t.OpString)
		b.WriteByte(' ')
		writeNode(b, t.Right)
		b.WriteByte(')')
	}
}
