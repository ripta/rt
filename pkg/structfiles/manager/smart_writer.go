package manager

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"text/template"
)

type WriteCloserFactory func(group *DocumentGroup) (io.WriteCloser, error)

func AlwaysWriter(w io.WriteCloser) WriteCloserFactory {
	return func(_ *DocumentGroup) (io.WriteCloser, error) {
		return w, nil
	}
}

type noCloseWriter struct {
	io.Writer
}

func (noCloseWriter) Close() error {
	return nil
}

func AlwaysNoCloseWriter(w io.Writer) WriteCloserFactory {
	return func(_ *DocumentGroup) (io.WriteCloser, error) {
		return noCloseWriter{w}, nil
	}
}

// DynamicFileWriter returns a new file handle for each write. The file name is
// generated by applying the given template to the input value. It is up to the
// caller to set the correct file mode and flags.
func DynamicFileWriter(filePattern string, fileFlag int, fileMode os.FileMode) (WriteCloserFactory, error) {
	tpl, err := template.New("file").Parse(filePattern)
	if err != nil {
		return nil, err
	}

	return func(dg *DocumentGroup) (io.WriteCloser, error) {
		buf := bytes.Buffer{}
		if err := tpl.Execute(&buf, dg); err != nil {
			return nil, err
		}

		fn := buf.String()
		if err := os.MkdirAll(filepath.Dir(fn), 0755); err != nil {
			return nil, err
		}

		h, err := os.OpenFile(fn, fileFlag, fileMode)
		if err != nil {
			return nil, err
		}

		return h, nil
	}, nil
}

// MemoryWriter writes to the given buffer. The buffer must not be nil.
func MemoryWriter(buf *bytes.Buffer) WriteCloserFactory {
	return func(_ *DocumentGroup) (io.WriteCloser, error) {
		return &noCloseWriter{buf}, nil
	}
}

// SingleFileWriter always returns the same file handle for writing. The handle
// is opened before the first write.
func SingleFileWriter(file string) (WriteCloserFactory, error) {
	if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
		return nil, err
	}

	h, err := os.OpenFile(file, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}

	return func(_ *DocumentGroup) (io.WriteCloser, error) {
		return h, nil
	}, nil
}
