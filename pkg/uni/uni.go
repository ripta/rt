package uni

import "github.com/spf13/cobra"

func NewCommand() *cobra.Command {
	c := cobra.Command{
		Use:          "uni",
		Short:        "Unicode commands",
		SilenceUsage: true,
	}

	c.AddCommand(newCategoriesCommand())
	c.AddCommand(newDescribeCommand())
	c.AddCommand(newListCommand())
	c.AddCommand(newMapCommand())
	c.AddCommand(newNFCCommand())
	c.AddCommand(newNFDCommand())
	c.AddCommand(newNFKCCommand())
	c.AddCommand(newNFKDCommand())
	c.AddCommand(newSortCommand())
	return &c
}
