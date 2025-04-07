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
			l.Errorf("unterminated quoted string")
			return nil

		case '"':
			done = true
			break
		}
	}

	l.Emit(tokens.LIT_STRING)
	return lexExpression
}

func lexRawString(l *L) lexingState {
	for done := false; !done; {
		switch l.Next() {
		case EOF, '\n':
			l.Errorf("unterminated raw string")
			return nil

		case '`':
			done = true
			break
		}
	}

	l.Emit(tokens.LIT_STRING)
	return lexExpression
}
