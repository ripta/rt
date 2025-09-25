package parser

import (
	"github.com/ripta/rt/pkg/calc/lexer"
	"github.com/ripta/rt/pkg/calc/tokens"
)

type P struct {
	lex   *lexer.L
	lit   string
	fn    parsingState
	peek  int
	poked []tokens.Token
}

func New(name, src string) *P {
	lex := lexer.New(name, src)
	p := &P{
		lex:   lex,
		fn:    parseInit,
		poked: []tokens.Token{},
	}

	return p
}

// Recursive descent parser for expressions
func parseExpr(p *P) parsingState {
	return nil
}

func (p *P) Peek() tokens.Token {
	if p.peek > 0 {
		return p.poked[p.peek-1]
	}

	p.peek = 1
	p.poked[0] = p.lex.NextToken()
	return p.poked[0]
}

func parseInit(p *P) parsingState {
	return parseExpr
}
