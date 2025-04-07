package lexer

type lexingState func(*L) lexingState
