package lipsum

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/time/rate"

	"github.com/ripta/rt/pkg/lipsum/data"
)

type Options struct {
	Paragraphs int
	RateLimit  int
}

func NewCommand() *cobra.Command {
	opts := &Options{}
	c := &cobra.Command{
		Use:   "lipsum",
		Short: "Generate placeholder text beyond Lorem Ipsum",
		Long:  "Generate placeholder text beyond Lorem Ipsum, optionally with rate-limited printing.",

		SilenceErrors: true,
		SilenceUsage:  true,

		RunE: opts.runner,
	}

	c.Flags().IntVarP(&opts.Paragraphs, "paragraphs", "p", 5, "Number of paragraphs to generate")
	c.Flags().IntVarP(&opts.RateLimit, "words-per-second", "w", 0, "Rate limit in words per second (0 for no limit)")

	return c
}

func (opts *Options) runner(cmd *cobra.Command, _ []string) error {
	info := data.Random()
	if info == nil {
		return errors.New("lipsum has no data")
	}

	// TODO(ripta): dump paragraphs at once if no rate limit is set
	limit := rate.Inf
	if opts.RateLimit > 0 {
		limit = rate.Limit(opts.RateLimit)
	}

	rl := rate.NewLimiter(limit, 1)

	// Generate paragraphs and print them, one word at a time, respecting the rate limit.
	for par := range opts.generateParagraphs(info) {
		words := strings.Fields(par)
		for idx, word := range words {
			if err := rl.WaitN(cmd.Context(), 1); err != nil {
				return fmt.Errorf("rate limit error: %w", err)
			}

			fmt.Print(word)
			if idx < len(words)-1 {
				fmt.Print(" ")
			}
		}

		fmt.Print("\n")
	}

	return nil
}

// generateParagraphs generates the specified number of paragraphs based on the
// provided data.Info. Each paragraph is taken in order, and sent over the returned
// channel.
//
// If the number of requested paragraphs exceeds the available paragraphs in info,
// it wraps around to the beginning, continuing until the requested number is met.
func (opts *Options) generateParagraphs(info *data.Info) <-chan string {
	out := make(chan string)
	go func() {
		defer close(out)

		totalParas := len(info.Paragraphs)
		for i := 0; i < opts.Paragraphs; i++ {
			para := info.Paragraphs[i%totalParas]
			out <- para
		}
	}()

	return out
}
