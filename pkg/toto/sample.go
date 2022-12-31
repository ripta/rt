package toto

import (
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/timestamppb"
	"os"

	"github.com/ripta/rt/samples/data/v1"
)

func newSampleCommand(t *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:  "sample",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, args []string) error {
			return runGenerateSample()
		},
	}

	return cmd
}

func runGenerateSample() error {
	ts := timestamppb.Now()
	m, err := anypb.New(ts)
	if err != nil {
		return err
	}

	env := v1.Envelope{
		KeyId:   "kid-123-sample",
		Message: m,
	}

	bs, err := proto.Marshal(&env)
	if err != nil {
		return err
	}

	_, err = os.Stdout.Write(bs)
	return err
}
