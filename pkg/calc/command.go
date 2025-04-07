package calc

import "github.com/spf13/cobra"

func NewCommand() *cobra.Command {
	c := &Calculator{}
	cmd := &cobra.Command{
		Use:   "calc",
		Short: "Calculate expressions",
		Long:  "Calculate expressions",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				c.REPL()
				return nil
			}

			for _, arg := range args {
				res, err := c.Evaluate(arg)
				if err != nil {
					return err
				}

				c.DisplayResult(res)
			}

			return nil
		},
	}

	return cmd
}
