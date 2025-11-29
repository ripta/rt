package lexer

import (
	"errors"
	"unicode"

	"github.com/ripta/rt/pkg/calc/tokens"
)

var ErrUnexpectedToken = errors.New("unexpected token")

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

	case r == '+':
		l.Emit(tokens.OP_PLUS)
		return lexExpression

	case r == '-':
		l.Emit(tokens.OP_MINUS)
		return lexExpression

	case r == '*':
		if l.Peek() == '*' {
			l.Next()
			l.Emit(tokens.OP_POW)
			return lexExpression
		}
		l.Emit(tokens.OP_STAR)
		return lexExpression

	case r == '/':
		l.Emit(tokens.OP_SLASH)
		return lexExpression

	case r == '%':
		l.Emit(tokens.OP_PERCENT)
		return lexExpression

	case r == '<':
		if l.Peek() == '<' {
			l.Next()
			l.Emit(tokens.OP_SHL)
			return lexExpression
		}
		return l.Errorf("%w %q in expression, expecting another '<'", ErrUnexpectedToken, string(r))

	case r == '>':
		if l.Peek() == '>' {
			l.Next()
			l.Emit(tokens.OP_SHR)
			return lexExpression
		}
		return l.Errorf("%w %q in expression, expecting another '>'", ErrUnexpectedToken, string(r))

	case r == '√':
		l.Emit(tokens.OP_ROOT)
		return lexExpression

	case r == '(':
		l.Emit(tokens.LPAREN)
		return lexExpression

	case r == ')':
		l.Emit(tokens.RPAREN)
		return lexExpression

	case IsAlnum(r):
		l.Rewind()
		return lexIdent

	default:
		return l.Errorf("%w %q in expression", ErrUnexpectedToken, string(r))
	}
}
