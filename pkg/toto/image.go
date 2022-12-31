package toto

import (
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
)

// CompileProto invokes protoc to generate a file descriptor set. The caller
// is responsible for calling Close() on the reader.
func CompileProto(root string) (io.ReadCloser, error) {
	files, err := FindProtoFiles(root)
	if err != nil {
		return nil, err
	}

	f, err := os.CreateTemp("", "proto.*.tmp")
	if err != nil {
		return nil, err
	}
	defer os.Remove(f.Name())

	args := []string{
		"--proto_path", root,
		"--descriptor_set_out", f.Name(),
	}
	args = append(args, files...)

	cmd := exec.Command("protoc", args...)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, err
	}

	return os.Open(f.Name())
}

func FindProtoFiles(root string) ([]string, error) {
	files := []string{}
	walker := func(path string, de fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if de.IsDir() {
			if de.Name() == ".git" {
				return fs.SkipDir
			}
			return nil
		}
		if filepath.Ext(de.Name()) == ".proto" {
			files = append(files, path)
		}
		return nil
	}

	if err := fs.WalkDir(os.DirFS(root), ".", walker); err != nil {
		return nil, err
	}

	return files, nil
}

// LoadImage loads a preprocessed FileDescriptorSet from an image. Said image
// could be generated using:
//
//	find . -name '*.proto' | xargs protoc--descriptor_set_out=all.image
//
// whereby 'all.image' would be passed into this function.
func LoadImage(r io.Reader) (*descriptorpb.FileDescriptorSet, error) {
	bs, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	fds := &descriptorpb.FileDescriptorSet{}
	if err := proto.Unmarshal(bs, fds); err != nil {
		return nil, err
	}

	return fds, nil
}
