package calc

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ripta/rt/pkg/calc/lexer"
	"github.com/ripta/rt/pkg/calc/tokens"
)

// rewriteResultRefs rewrites each $N reference in expr from the file's line numbers to the session
// numbers they were bound to. It works on lexer tokens, so a $ inside a string is left alone.
//
// A reference with no mapping fails here instead of being left in place. Left in place, it would
// resolve to whatever result the session holds at that number, which is a different line's value.
func rewriteResultRefs(expr string, num *loadNumbering) (string, error) {
	var refs []tokens.Token

	// The lexer runs in a goroutine, so the channel is always drained to the end.
	for tok := range lexer.New("(eval)", expr).Tokens() {
		switch {
		case tok.Type == tokens.ILLEGAL:
			// Evaluate reports the lexing error.
			return expr, nil
		case tok.Type == tokens.IDENT && strings.HasPrefix(tok.Value, "$"):
			refs = append(refs, tok)
		}
	}

	var out strings.Builder

	last := 0
	for _, tok := range refs {
		line, err := strconv.Atoi(tok.Value[1:])
		session, ok := num.session(line)
		if err != nil || !ok {
			return "", fmt.Errorf("%s: no result for line %s", tok.Value, tok.Value[1:])
		}

		start := tok.Pos.Column - 1
		out.WriteString(expr[last:start])
		out.WriteString("$" + strconv.Itoa(session))
		last = start + len(tok.Value)
	}

	out.WriteString(expr[last:])
	return out.String(), nil
}
