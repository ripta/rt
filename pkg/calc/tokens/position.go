package tokens

import "fmt"

type Position struct {
	File   string
	Line   int
	Column int
}

func (p Position) IsZero() bool {
	return p == Position{}
}

func (p Position) String() string {
	return fmt.Sprintf("%s:%d:%d", p.File, p.Line, p.Column)
}
