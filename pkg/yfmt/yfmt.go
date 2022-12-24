package yfmt

import (
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
	"io"
	"os"
)

type formatter struct {
	Indent int
}

func NewCommand() *cobra.Command {
	f := &formatter{
		Indent: 2,
	}

	c := cobra.Command{
		Use:   "yfmt",
		Short: "Format YAML",
		RunE: func(_ *cobra.Command, args []string) error {
			return f.run(args)
		},
	}

	c.PersistentFlags().IntVarP(&f.Indent, "indent", "i", f.Indent, "Number of spaces for indent")
	return &c
}

func (f *formatter) process(out io.Writer, in io.Reader) error {
	dec := yaml.NewDecoder(in)

	enc := yaml.NewEncoder(out)
	if f.Indent > 0 {
		enc.SetIndent(f.Indent)
	}
	defer enc.Close()

	for {
		val := yaml.Node{}
		if err := dec.Decode(&val); err != nil {
			if err == io.EOF {
				break
			}
			return err
		}

		if err := enc.Encode(&val); err != nil {
			return err
		}
	}

	return nil
}

func (f *formatter) run(files []string) error {
	if len(files) == 0 {
		return f.process(os.Stdout, os.Stdin)
	}

	for _, file := range files {
		in, err := os.Open(file)
		if err != nil {
			return err
		}

		if err := f.process(os.Stdout, in); err != nil {
			return err
		}
	}

	return nil
}
