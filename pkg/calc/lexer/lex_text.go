package lexer

import (
	"strings"

	"github.com/ripta/rt/pkg/calc/tokens"
)

func lexIdent(l *L) lexingState {
	l.AcceptWhile(IsAlnum)
	l.Emit(tokens.IDENT)
	return lexExpression
}

func lexLineIdent(l *L) lexingState {
	if !l.AcceptWhile(func(r rune) bool { return r >= '0' && r <= '9' }) {
		return l.Errorf("expected digits after '$'")
	}

	l.Emit(tokens.IDENT)
	return lexExpression
}

func lexNumber(l *L) lexingState {
	// A '.' that starts a '..' belongs to the range token, not the number.
	numeric := func(r rune) bool {
		if r == '.' && strings.HasPrefix(l.src[l.pos:], ".") {
			return false
		}
		return IsNumeric(r)
	}
	if !l.AcceptWhile(numeric) {
		return l.Errorf("invalid number: %s", l.Current())
	}

	num := l.Current()
	if dec := strings.Count(num, "."); dec > 1 {
		return l.Errorf("too many decimal points (%d) in number; expected 0 or 1", dec)
	} else if dec == 1 {
		l.Emit(tokens.LIT_FLOAT)
	} else {
		l.Emit(tokens.LIT_INT)
	}

	return lexExpression
}
