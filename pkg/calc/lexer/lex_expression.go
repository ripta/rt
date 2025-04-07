package lexer

import (
	"unicode"

	"github.com/ripta/rt/pkg/calc/tokens"
)

func lexExpression(l *L) lexingState {
	switch r := l.Next(); {

	case r == EOF:
		l.eof = true
		return nil

	case unicode.IsSpace(r):
		l.AcceptWhile(unicode.IsSpace)
		l.Emit(tokens.WHITESPACE)
		return lexExpression

	case r == '"':
		return lexQuotedString

	case r == '`':
		return lexRawString

	case r == '=':
		l.Emit(tokens.ASSIGN)
		return lexExpression

	case unicode.IsDigit(r):
		l.Rewind()
		return lexNumber

	case r == '-':
		l.Emit(tokens.OP_MINUS)
		return lexExpression

	case IsAlnum(r):
		l.Rewind()
		return lexIdent

	default:
		l.Errorf("unexpected token %s", string(r))
		return nil
	}
}
