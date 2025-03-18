package structfiles

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func generateDiff(preName, postName string, preBuf, postBuf *bytes.Buffer) (string, error) {
	preName = strings.ReplaceAll(preName, "/", "_")
	postName = strings.ReplaceAll(postName, "/", "_")

	dir, err := os.MkdirTemp("", "structfiles-differ-*")
	if err != nil {
		return "", fmt.Errorf("creating temporary directory: %w", err)
	}

	defer os.RemoveAll(dir)

	preFile := filepath.Join(dir, preName)
	if err := os.WriteFile(preFile, preBuf.Bytes(), 0600); err != nil {
		return "", fmt.Errorf("writing pre file: %w", err)
	}

	postFile := filepath.Join(dir, postName)
	if err := os.WriteFile(postFile, postBuf.Bytes(), 0600); err != nil {
		return "", fmt.Errorf("writing post file: %w", err)
	}

	outBuf := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}

	cmd := exec.Command("diff", "-u", preName, postName)
	cmd.Dir = dir
	cmd.Stdout = outBuf
	cmd.Stderr = errBuf

	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		// Exit codes are meaningful: 0 means no difference, 1 means difference, >1 means error
		if errors.As(err, &ee) && ee.ExitCode() > 1 {
			if errBuf.Len() > 0 {
				return "", fmt.Errorf("running diff: %w, with underlying error %s", err, errBuf.String())
			}

			return "", fmt.Errorf("running diff: %w with no other detail", err)
		}
	}

	return outBuf.String(), nil
}
