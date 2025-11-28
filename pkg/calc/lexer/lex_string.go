package lexer

import "github.com/ripta/rt/pkg/calc/tokens"

func lexQuotedString(l *L) lexingState {
	for done := false; !done; {
		switch l.Next() {
		case '\\':
			if r := l.Next(); r != EOF && r != '\n' {
				break
			}
			fallthrough

		case EOF, '\n':
			return l.Errorf("unterminated quoted string")

		case '"':
			done = true
		}
	}

	l.Emit(tokens.LIT_STRING)
	return lexExpression
}

func lexRawString(l *L) lexingState {
	for done := false; !done; {
		switch l.Next() {
		case EOF, '\n':
			return l.Errorf("unterminated raw string")

		case '`':
			done = true
		}
	}

	l.Emit(tokens.LIT_STRING)
	return lexExpression
}
