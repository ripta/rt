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

func lexNumber(l *L) lexingState {
	if !l.AcceptWhile(IsNumeric) {
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
