package enc

import "github.com/spf13/cobra"

func AddCommands(c *cobra.Command) *cobra.Command {
	c.AddCommand(newBase64Command())
	return c
}

func NewCommand() *cobra.Command {
	c := cobra.Command{
		Use:   "enc",
		Short: "Encoding / decoding commands",
	}

	return AddCommands(&c)
}
