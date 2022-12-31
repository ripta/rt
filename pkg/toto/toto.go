package toto

import "github.com/spf13/cobra"

type options struct {
	Path string

	Format       OutputFormat
	AllowPartial bool

	// JSON options
	UseProtoNames   bool
	UseEnumNumbers  bool
	EmitUnpopulated bool

	// Text options
	EmitASCII   bool
	EmitUnknown bool
}

func NewCommand() *cobra.Command {
	t := &options{
		Path:   ".",
		Format: TextOutputFormat,
	}

	c := cobra.Command{
		Use:           "toto",
		Short:         "Proto commands",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	c.AddCommand(newCompileCommand(t))
	c.AddCommand(newRecodeCommand(t))
	c.AddCommand(newSampleCommand(t))
	return &c
}
