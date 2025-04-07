package calc

import (
	"fmt"
	"strings"

	"github.com/ripta/rt/pkg/calc/lexer"
	"github.com/ripta/rt/pkg/num"
)

func Evaluate(expr string) (*num.Num, error) {
	return evaluate(strings.TrimSpace(expr))
}

func evaluate(expr string) (*num.Num, error) {
	l := lexer.New("(eval)", expr)
	for tok := range l.Tokens() {
		fmt.Printf("%+v\n", tok)
	}

	if l.Err() != nil {
		return nil, l.Err()
	}

	return num.Zero(), nil
}
