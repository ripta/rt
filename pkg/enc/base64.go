package enc

import (
	"fmt"

	"github.com/spf13/cobra"
)

type base64Coder struct {}

func newBase64Command() *cobra.Command {
	r := &base64Coder{}
	c := cobra.Command{
		Use:         "base64",
		Aliases:     []string{"b64"},
		Description: "Base64",
		RunE: r.run,
	}

	return &c
}

func (r *base64Coder) run(cmd *cobra.Command, args []string) error {
	fmt.Println(cmd.Name())
	return nil
}
