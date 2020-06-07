package uni

import "github.com/spf13/cobra"

func NewCommand() *cobra.Command {
	c := cobra.Command{
		Use:   "uni",
		Short: "Unicode commands",
	}
	c.AddCommand(newDescribeCommand())
	c.AddCommand(newListCommand())
	c.AddCommand(newMapCommand())
	return &c
}
