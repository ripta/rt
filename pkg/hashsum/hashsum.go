package hashsum

import (
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"fmt"
	"hash"
	"io"
	"lukechampine.com/blake3"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/crypto/sha3"
)

type Hasher struct {
	Aliases  []string
	HashFunc func() hash.Hash
}

var hashers = map[string]Hasher{
	"blake3/256": {
		HashFunc: func() hash.Hash { return blake3.New(32, nil) },
	},
	"blake3/512": {
		Aliases:  []string{"blake3"},
		HashFunc: func() hash.Hash { return blake3.New(64, nil) },
	},
	"sha1": {
		Aliases:  []string{"sha-1"},
		HashFunc: sha1.New,
	},
	"sha224": {
		Aliases:  []string{"sha-224"},
		HashFunc: sha256.New224,
	},
	"sha256": {
		Aliases:  []string{"sha-256"},
		HashFunc: sha256.New,
	},
	"sha3": {
		Aliases:  []string{"sha-3/512"},
		HashFunc: sha3.New512,
	},
	"sha384": {
		Aliases:  []string{"sha-384"},
		HashFunc: sha512.New384,
	},
	"sha512": {
		Aliases:  []string{"sha-512"},
		HashFunc: sha512.New,
	},
}

func NewCommand() *cobra.Command {
	c := cobra.Command{
		Use:     "hashsum",
		Aliases: []string{"hash", "hs"},
		Short:   "Run a hash function against the input and output binary hash",
		Long: `Run a hash function against the input and output binary hash.
Pipe the output to "enc hex" to get a hex-encoded hash.`,
		Example: `echo -n "hello" | hs sha256 | enc hex`,

		SilenceUsage:  true,
		SilenceErrors: true,
	}

	for name, hasher := range hashers {
		sub := &cobra.Command{
			Use:     name,
			Aliases: hasher.Aliases,
			Short:   fmt.Sprintf("Hashing function %s", name),
			Args:    cobra.NoArgs,
			RunE:    generateRunner(hasher.HashFunc),
		}

		c.AddCommand(sub)
	}

	return &c
}

func generateRunner(hf func() hash.Hash) func(*cobra.Command, []string) error {
	w, r := os.Stdout, os.Stdin

	return func(cmd *cobra.Command, args []string) error {
		e := hf()
		if _, err := io.Copy(e, r); err != nil {
			return err
		}
		if _, err := w.Write(e.Sum(nil)); err != nil {
			return err
		}
		return nil
	}
}
