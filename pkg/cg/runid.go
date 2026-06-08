package cg

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Crockford base-32 alphabet (excludes I, L, O, U). Used to encode run IDs.
const runIDAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// Run ID parameters: six characters of Crockford base-32 give ~30 bits of
// entropy, enough to keep collisions rare for the cleanup cap. The retry cap
// guards against pathological loops if randomness ever degrades.
const (
	runIDLen      = 6
	runIDAttempts = 10
)

// idSource generates a single run ID. Indirected to keep tests deterministic.
var idSource = generateRunID

// generateRunID returns a fresh 6-character Crockford base-32 ID.
func generateRunID() (string, error) {
	var b [runIDLen]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("reading randomness: %w", err)
	}
	out := make([]byte, runIDLen)
	for i, x := range b {
		out[i] = runIDAlphabet[int(x)&0x1f]
	}
	return string(out), nil
}

// newRunDir allocates a unique run ID under parent and creates the directory.
// On collision with an existing directory it regenerates the ID, up to
// runIDAttempts tries.
func newRunDir(parent string) (id, dir string, err error) {
	for range runIDAttempts {
		id, err = idSource()
		if err != nil {
			return "", "", err
		}
		dir = filepath.Join(parent, id)
		err = os.Mkdir(dir, 0o755)
		if err == nil {
			return id, dir, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return "", "", fmt.Errorf("creating run dir: %w", err)
		}
	}
	return "", "", fmt.Errorf("could not allocate unique run id after %d attempts", runIDAttempts)
}
