package lexer

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/ripta/rt/pkg/calc/tokens"
)

const EOF rune = -1

type L struct {
	// name is the name of the source, used in error reporting.
	name string
	// src is the source string to be lexed.
	src string

	// start is the start position of the current token
	start int
	// pos is the current position of the current token
	pos int
	// eof is true if the end of the source has been reached
	eof bool
	// line is the current line number
	line int

	// err is the error encountered during lexing
	err error
	// tokens is the channel of tokens emitted by the lexer
	tokens chan tokens.Token
}

func New(name, src string) *L {
	l := &L{
		name:   name,
		src:    src,
		line:   1,
		tokens: make(chan tokens.Token, 100),
	}

	go l.run()
	return l
}

// AcceptOnce accepts a single rune if the predicate is true. Returns true when
// a rune is accepted. Otherwise, the state is rewound and false is returned.
func (l *L) AcceptOnce(pred func(rune) bool) bool {
	if pred(l.Next()) {
		return true
	}

	l.Rewind()
	return false
}

// AcceptWhile accepts runes while the predicate is true. Returns true when at
// least one rune is accepted. Otherwise, the state is rewound and returns false.
func (l *L) AcceptWhile(pred func(rune) bool) bool {
	count := 0
	for pred(l.Next()) {
		count++
	}

	l.Rewind()
	return count > 0
}

// Current returns the current token.
func (l *L) Current() string {
	return l.src[l.start:l.pos]
}

func (l *L) Emit(t tokens.TokenType) {
	if l.start == l.pos {
		l.Errorf("trying to emit empty %s token at %d:%d", t, l.line, l.start)
		return
	}

	l.tokens <- tokens.Token{
		Type:  t,
		Value: l.src[l.start:l.pos],
		Pos: tokens.Position{
			File:   l.name,
			Line:   l.line,
			Column: l.start + 1,
		},
	}
	l.start = l.pos
}

func (l *L) Errorf(format string, args ...any) {
	err := fmt.Errorf(format, args...)
	pos := tokens.Position{
		File:   l.name,
		Line:   l.line,
		Column: l.start + 1,
	}

	l.err = fmt.Errorf("%s: %w", pos, err)
	l.tokens <- tokens.Token{
		Type:  tokens.ILLEGAL,
		Value: l.src[l.start:l.pos],
		Err:   err,
		Pos:   pos,
	}

	l.eof = true
}

func (l *L) Err() error {
	return l.err
}

// Next returns the next rune from the source. It advances the position.
func (l *L) Next() rune {
	if l.err != nil {
		return EOF
	}
	if l.pos >= len(l.src) {
		l.eof = true
		return EOF
	}

	// fmt.Printf("NEXT: start=%d, pos=%d, src=%q\n", l.start, l.pos, l.src[l.start:l.pos])
	rv, rl := utf8.DecodeRuneInString(l.src[l.pos:])
	l.pos += rl
	if rv == '\n' {
		l.line++
	}
	return rv
}

// Peek returns the next rune without advancing the position.
func (l *L) Peek() rune {
	defer l.Rewind()
	return l.Next()
}

// Rewind unreads the last rune read from the source.
func (l *L) Rewind() {
	// no rewinding past the start
	if l.eof || l.pos == 0 {
		return
	}

	rv, rl := utf8.DecodeLastRuneInString(l.src[:l.pos])
	l.pos -= rl
	if rv == '\n' {
		l.line--
	}
}

func (l *L) run() {
	for st := lexExpression; st != nil; {
		st = st(l)
	}
	close(l.tokens)
}

func (l *L) Skip() {
	l.line += strings.Count(l.src[l.start:l.pos], "\n")
	l.start = l.pos
}

// Tokens returns a channel of tokens. The channel is closed when the lexer
// reaches the end of the source.
func (l *L) Tokens() <-chan tokens.Token {
	return l.tokens
}
