package parser

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ripta/rt/pkg/calc/lexer"
	"github.com/ripta/rt/pkg/calc/tokens"
)

type P struct {
	lex *lexer.L
	fn  parsingState

	root Node
	err  error

	buf []tokens.Token
}

func New(name, src string) *P {
	return &P{
		lex: lexer.New(name, src),
		fn:  parseInit,
	}
}

func Parse(name, src string) (Node, error) {
	return New(name, src).Parse()
}

func (p *P) Parse() (Node, error) {
	for state := p.fn; state != nil; {
		state = state(p)
	}
	if p.err != nil {
		return nil, p.err
	}
	if p.root == nil {
		return nil, fmt.Errorf("no expression parsed")
	}
	return p.root, nil
}

func parseExpr(p *P) parsingState {
	if p.err != nil {
		return nil
	}
	node, err := p.parseAssignment()
	if err != nil {
		p.err = err
		return nil
	}
	tok := p.next()
	if p.err != nil {
		return nil
	}
	if tok.Type != tokens.EOF {
		p.err = p.errorf(tok, "unexpected token %s", tok.Type)
		return nil
	}
	p.root = node
	return nil
}

func parseInit(p *P) parsingState {
	return parseExpr
}

func (p *P) parseAssignment() (Node, error) {
	if p.err != nil {
		return nil, p.err
	}
	left, err := p.parseAdditive()
	if err != nil {
		return nil, err
	}

	ident, ok := left.(*IdentNode)
	if !ok {
		return left, nil
	}

	tok := p.peek()
	if p.err != nil {
		return nil, p.err
	}
	if tok.Type == tokens.ASSIGN {
		p.next() // consume =
		right, err := p.parseAssignment()
		if err != nil {
			return nil, err
		}
		return &AssignNode{
			Name:  ident.Name,
			Value: right,
		}, nil
	}

	return left, nil
}

func (p *P) parseAdditive() (Node, error) {
	if p.err != nil {
		return nil, p.err
	}
	node, err := p.parseMultiplicative()
	if err != nil {
		return nil, err
	}

	for {
		tok := p.peek()
		if p.err != nil {
			return nil, p.err
		}
		if tok.Type != tokens.OP_PLUS && tok.Type != tokens.OP_MINUS {
			break
		}
		p.next()
		right, err := p.parseMultiplicative()
		if err != nil {
			return nil, err
		}
		node = &BinaryNode{
			Op:    tok,
			Left:  node,
			Right: right,
		}
	}

	return node, nil
}

func (p *P) parseMultiplicative() (Node, error) {
	if p.err != nil {
		return nil, p.err
	}
	node, err := p.parseUnary()
	if err != nil {
		return nil, err
	}

	for {
		tok := p.peek()
		if p.err != nil {
			return nil, p.err
		}
		if tok.Type != tokens.OP_STAR && tok.Type != tokens.OP_SLASH {
			break
		}
		p.next()
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		node = &BinaryNode{
			Op:    tok,
			Left:  node,
			Right: right,
		}
	}

	return node, nil
}

func (p *P) parseUnary() (Node, error) {
	if p.err != nil {
		return nil, p.err
	}

	tok := p.peek()
	if p.err != nil {
		return nil, p.err
	}

	if tok.Type == tokens.OP_MINUS {
		p.next()
		expr, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &UnaryNode{
			Op:   tok,
			Expr: expr,
		}, nil
	}

	return p.parsePrimary()
}

func (p *P) parsePrimary() (Node, error) {
	if p.err != nil {
		return nil, p.err
	}

	tok := p.next()
	if p.err != nil {
		return nil, p.err
	}

	switch tok.Type {
	case tokens.LIT_INT, tokens.LIT_FLOAT:
		val, err := p.parseNumber(tok)
		if err != nil {
			return nil, err
		}
		return &NumberNode{Value: val}, nil

	case tokens.IDENT:
		return &IdentNode{Name: tok}, nil

	case tokens.LPAREN:
		node, err := p.parseAssignment()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(tokens.RPAREN); err != nil {
			return nil, err
		}
		return node, nil

	case tokens.EOF:
		return nil, p.errorf(tok, "unexpected EOF")

	default:
		return nil, p.errorf(tok, "unexpected token %s", tok.Type)
	}
}

func (p *P) parseNumber(tok tokens.Token) (float64, error) {
	cleaned := strings.ReplaceAll(tok.Value, "_", "")
	val, err := strconv.ParseFloat(cleaned, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: invalid number %q: %w", tok.Pos, tok.Value, err)
	}
	return val, nil
}

func (p *P) next() tokens.Token {
	tok := p.nextRaw()
	for tok.Type == tokens.WHITESPACE {
		tok = p.nextRaw()
	}

	if tok.Type == tokens.ILLEGAL && p.err == nil {
		if tok.Err != nil {
			p.err = fmt.Errorf("%s: %w", tok.Pos, tok.Err)
		} else {
			p.err = fmt.Errorf("%s: illegal token %q", tok.Pos, tok.Value)
		}
	}

	return tok
}

func (p *P) peek() tokens.Token {
	tok := p.next()
	p.unread(tok)
	return tok
}

func (p *P) unread(tok tokens.Token) {
	p.buf = append(p.buf, tok)
}

func (p *P) nextRaw() tokens.Token {
	if n := len(p.buf); n > 0 {
		tok := p.buf[n-1]
		p.buf = p.buf[:n-1]
		return tok
	}

	tok := p.lex.NextToken()
	if tok.Type == 0 && tok.Value == "" && tok.Pos == (tokens.Position{}) && tok.Err == nil {
		tok.Type = tokens.EOF
	}

	return tok
}

func (p *P) expect(tt tokens.TokenType) (tokens.Token, error) {
	tok := p.next()
	if p.err != nil {
		return tokens.Token{}, p.err
	}
	if tok.Type != tt {
		return tok, p.errorf(tok, "expected %s, got %s", tt, tok.Type)
	}

	return tok, nil
}

func (p *P) errorf(tok tokens.Token, format string, args ...any) error {
	msg := fmt.Sprintf(format, args...)
	if !tok.Pos.IsZero() {
		return fmt.Errorf("%s: %s", tok.Pos, msg)
	}

	return fmt.Errorf("%s", msg)
}
