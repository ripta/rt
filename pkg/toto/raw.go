package toto

import (
	"fmt"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
	"io"
	"os"
)

func newRawCommand(t *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:  "raw",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runRaw()
		},
	}
	return cmd
}

func runRaw() error {
	bs, err := io.ReadAll(os.Stdin)
	if err != nil {
		return err
	}

	m := emptypb.Empty{}
	if err := proto.Unmarshal(bs, &m); err != nil {
		return err
	}

	opts := prototext.MarshalOptions{
		Multiline:    true,
		AllowPartial: true,
		EmitUnknown:  true,
		Indent:       "  ",
	}

	out, err := opts.Marshal(&m)
	if err != nil {
		return err
	}

	fmt.Println(string(out))
	return nil
}
