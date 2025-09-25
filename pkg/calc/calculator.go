package calc

import (
	"fmt"
	"os"

	"github.com/elk-language/go-prompt"

	"github.com/ripta/rt/pkg/num"
)

type Calculator struct {
	ExpressionCount int
}

func (c *Calculator) Evaluate(expr string) (*num.Num, error) {
	return Evaluate(expr)
}

func (c *Calculator) Execute(expr string) {
	defer func() {
		c.ExpressionCount++
		fmt.Println()
	}()

	res, err := c.Evaluate(expr)
	if err != nil {
		c.DisplayError(err)
		return
	}

	c.DisplayResult(res)
}

func (c *Calculator) DisplayError(err error) {
	fmt.Fprintf(os.Stderr, "calc:%03d/ Error: %s\n", c.ExpressionCount, err)
}

func (c *Calculator) DisplayResult(res *num.Num) {
	fmt.Printf("%s\n", res)
}

func (c *Calculator) REPL() {
	p := prompt.New(
		c.Execute,
		prompt.WithPrefixCallback(func() string {
			return fmt.Sprintf("calc:%03d> ", c.ExpressionCount)
		}),
		prompt.WithExitChecker(func(in string, breakline bool) bool {
			return breakline && (in == "exit" || in == "quit")
		}),
	)

	fmt.Println("calc: ^D to exit")
	p.Run()

	fmt.Println("calc: goodbye")
}
