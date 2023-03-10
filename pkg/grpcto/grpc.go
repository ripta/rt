package grpcto

import (
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/ripta/rt/pkg/grpcto/grpcframing"
)

type options struct {
	MaxBytes int
}

func NewCommand() *cobra.Command {
	g := &options{
		MaxBytes: 1 * 1024 * 1024,
	}

	c := &cobra.Command{
		Use:           "grpcto",
		Short:         "gRPC commands",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	c.AddCommand(newFrameCommand(g))
	c.AddCommand(newUnframeCommand(g))

	return c
}

func newFrameCommand(g *options) *cobra.Command {
	return &cobra.Command{
		Use:   "frame",
		Short: "Frame a set of bytes (probably proto) in gRPC",
		RunE: func(_ *cobra.Command, _ []string) error {
			bs, err := io.ReadAll(io.LimitReader(os.Stdin, int64(g.MaxBytes)))
			if err != nil {
				return err
			}

			p, err := grpcframing.New(bs)
			if err != nil {
				return err
			}

			if _, err := p.WriteTo(os.Stdout); err != nil {
				return err
			}

			return nil
		},
	}
}

func newUnframeCommand(g *options) *cobra.Command {
	return &cobra.Command{
		Use:   "unframe",
		Short: "Unframe gRPC back to a set of bytes (probably proto)",
		RunE: func(_ *cobra.Command, _ []string) error {
			p, err := grpcframing.DecodeReader(os.Stdin, g.MaxBytes)
			if err != nil {
				return err
			}

			n, err := os.Stdout.Write(p.Message())
			if err != nil {
				return err
			}

			if p.Len() > n {
				return io.ErrShortWrite
			} else if p.Len() < n {
				return io.ErrShortBuffer
			}

			return nil
		},
	}
}
