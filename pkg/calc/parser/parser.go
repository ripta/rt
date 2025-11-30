package parser

import (
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/ripta/reals/pkg/constructive"
	"github.com/ripta/reals/pkg/rational"
	"github.com/ripta/reals/pkg/unified"

	"github.com/ripta/rt/pkg/calc/lexer"
	"github.com/ripta/rt/pkg/calc/tokens"
)

var ErrUnexpectedToken = errors.New("unexpected token")

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
		p.err = p.errorf(tok, "%s %s, expecting EOF", ErrUnexpectedToken, tok.Type)
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

	// Check if left is an identifier (possibly wrapped in comments)
	var ident *IdentNode
	var comments []*CommentNode
	node := left

	// Unwrap any comment layers
	for {
		if comment, ok := node.(*CommentNode); ok {
			comments = append(comments, comment)
			node = comment.Expr
		} else {
			break
		}
	}

	ident, ok := node.(*IdentNode)
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
		assignNode := &AssignNode{
			Name:  ident.Name,
			Value: right,
		}

		// Re-wrap with comments in reverse order
		var result Node = assignNode
		for i := len(comments) - 1; i >= 0; i-- {
			result = &CommentNode{
				Text: comments[i].Text,
				Tok:  comments[i].Tok,
				Expr: result,
			}
		}
		return result, nil
	}

	return left, nil
}

func (p *P) parseComment() (Node, error) {
	commentTok := p.next() // Consume LIT_STRING

	// Parse the expression this comment wraps
	expr, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}

	return &CommentNode{
		Text: extractCommentText(commentTok),
		Tok:  commentTok,
		Expr: expr,
	}, nil
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
	node, err := p.parseExponential()
	if err != nil {
		return nil, err
	}

	for {
		tok := p.peek()
		if p.err != nil {
			return nil, p.err
		}
		if tok.Type != tokens.OP_STAR && tok.Type != tokens.OP_SLASH && tok.Type != tokens.OP_PERCENT && tok.Type != tokens.OP_SHL && tok.Type != tokens.OP_SHR {
			break
		}
		p.next()
		right, err := p.parseExponential()
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

func (p *P) parseExponential() (Node, error) {
	if p.err != nil {
		return nil, p.err
	}
	node, err := p.parseUnary()
	if err != nil {
		return nil, err
	}

	tok := p.peek()
	if p.err != nil {
		return nil, p.err
	}
	if tok.Type == tokens.OP_POW {
		p.next()
		// Right-associative: parse the right side recursively
		right, err := p.parseExponential()
		if err != nil {
			return nil, err
		}
		return &BinaryNode{
			Op:    tok,
			Left:  node,
			Right: right,
		}, nil
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

	if tok.Type == tokens.OP_MINUS || tok.Type == tokens.OP_ROOT {
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

	// Check for leading comment first
	if p.peek().Type == tokens.LIT_STRING {
		return p.parseComment()
	}

	tok := p.next()
	if p.err != nil {
		return nil, p.err
	}

	var node Node
	switch tok.Type {
	case tokens.LIT_INT, tokens.LIT_FLOAT:
		val, err := p.parseNumber(tok)
		if err != nil {
			return nil, err
		}
		node = &NumberNode{Value: val}

	case tokens.IDENT:
		node = &IdentNode{Name: tok}

	case tokens.LPAREN:
		var err error
		node, err = p.parseAssignment()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(tokens.RPAREN); err != nil {
			return nil, err
		}

	case tokens.EOF:
		return nil, p.errorf(tok, "unexpected EOF")

	default:
		return nil, p.errorf(tok, "unexpected token %s", tok.Type)
	}

	// Check for trailing comment
	if p.peek().Type == tokens.LIT_STRING {
		commentTok := p.next()
		node = &CommentNode{
			Text: extractCommentText(commentTok),
			Tok:  commentTok,
			Expr: node,
		}
	}

	return node, nil
}

func (p *P) parseNumber(tok tokens.Token) (*unified.Real, error) {
	cleaned := strings.ReplaceAll(tok.Value, "_", "")
	rat := new(big.Rat)
	if _, ok := rat.SetString(cleaned); !ok {
		return nil, fmt.Errorf("%s: invalid number %q", tok.Pos, tok.Value)
	}

	return unified.New(constructive.One(), rational.FromRational(rat)), nil
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
