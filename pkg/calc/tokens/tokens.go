package tokens

import "fmt"

type Token struct {
	Type  TokenType `json:"type"`
	Value string    `json:"value"`
	Pos   Position  `json:"pos"`
	Err   error     `json:"error"`
}

func (t Token) String() string {
	switch t.Type {
	case EOF:
		return "<EOF>"
	case ILLEGAL:
		return fmt.Sprintf("<ILLEGAL:%q %s error=%v>", t.Value, t.Pos, t.Err)
	case IDENT, ASSIGN, LIT_INT, LIT_FLOAT:
		return fmt.Sprintf("<%s:%q %s>", t.Type, t.Value, t.Pos)
	default:
		if len(t.Value) > 10 {
			return fmt.Sprintf("<%s:%.10q len=%d>", t.Type, t.Value, len(t.Value))
		}
		return fmt.Sprintf("<%s:%q len=%d>", t.Type, t.Value, len(t.Value))
	}
}

type TokenType int

const (
	EOF        TokenType = iota // End of file
	ILLEGAL                     // Illegal token
	WHITESPACE                  // Whitespace

	IDENT  // Identifier
	ASSIGN // Assignment operator (=)

	LIT_INT    // Integer literal
	LIT_FLOAT  // Float literal
	LIT_DEGREE // Degree literal
	LIT_STRING // String literal

	OP_PLUS    // Infix addition (+)
	OP_MINUS   // Infix subtraction (-)
	OP_STAR    // Infix multiplication (*)
	OP_SLASH   // Infix division (/)
	OP_PERCENT // Infix modulo (%)
	OP_ROOT    // Root operator (√)
	OP_SHL     // Left shift (<<)
	OP_SHR     // Right shift (>>)
	OP_POW     // Exponentiation (**)

	LPAREN // (
	RPAREN // )
)

var tokenNames = map[TokenType]string{
	EOF:        "EOF",
	ILLEGAL:    "ILLEGAL",
	WHITESPACE: "WHITESPACE",

	IDENT:  "IDENT",
	ASSIGN: "ASSIGN",

	LIT_INT:    "LIT_INT",
	LIT_FLOAT:  "LIT_FLOAT",
	LIT_DEGREE: "LIT_DEGREE",
	LIT_STRING: "LIT_STRING",

	OP_PLUS:    "OP_PLUS",
	OP_MINUS:   "OP_MINUS",
	OP_STAR:    "OP_STAR",
	OP_SLASH:   "OP_SLASH",
	OP_PERCENT: "OP_PERCENT",
	OP_ROOT:    "OP_ROOT",
	OP_SHL:     "OP_SHL",
	OP_SHR:     "OP_SHR",
	OP_POW:     "OP_POW",

	LPAREN: "LPAREN",
	RPAREN: "RPAREN",
}

func (t TokenType) String() string {
	if name, ok := tokenNames[t]; ok {
		return name
	}
	return fmt.Sprintf("UNKNOWN(%d)", t)
}
