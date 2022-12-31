package toto

import (
	"fmt"
	"github.com/spf13/cobra"
	"io"
	"log"
	"os"
	"path/filepath"
)

func newCompileCommand(t *options) *cobra.Command {
	return &cobra.Command{
		Use: "compile",
		RunE: func(_ *cobra.Command, args []string) error {
			if len(args) < 1 {
				return fmt.Errorf("at least one directory containing proto files is required")
			}

			for _, arg := range args {
				fp, err := filepath.Abs(arg)
				if err != nil {
					return err
				}

				fi, err := os.Stat(fp)
				if err != nil {
					return err
				}

				if !fi.IsDir() {
					return fmt.Errorf("expected %q to be a directory", fp)
				}

				log.Printf("Processing %s\n", fp)
				r, err := CompileProto(fp)
				if err != nil {
					return err
				}

				cache := filepath.Join(fp, ".file_descriptor_set")
				file, err := os.OpenFile(cache, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
				if err != nil {
					return err
				}

				log.Printf("Writing compiled file to %s\n", cache)
				if _, err := io.Copy(file, r); err != nil {
					return err
				}

				if err := r.Close(); err != nil {
					return err
				}
				if err := file.Close(); err != nil {
					return err
				}
			}
			return nil
		},
	}
}
