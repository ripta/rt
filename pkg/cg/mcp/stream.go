package mcp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ripta/rt/pkg/cg"
)

const (
	defaultStreamBytes = 16384
	maxStreamBytes     = 1 << 20

	streamFromHead = "head"
	streamFromTail = "tail"
)

// streamInput is the argument shape for `cg_stdout` and `cg_stderr`.
type streamInput struct {
	ID       string `json:"id" jsonschema:"capture run ID"`
	MaxBytes int    `json:"max_bytes,omitempty" jsonschema:"maximum bytes to return; default 16384, max 1048576"`
	From     string `json:"from,omitempty" jsonschema:"\"head\" (default) reads from offset; \"tail\" reads the last max_bytes"`
	Offset   int64  `json:"offset,omitempty" jsonschema:"byte offset for head reads; ignored when from=tail"`
}

// streamOutput is the result shape for `cg_stdout` and `cg_stderr`.
type streamOutput struct {
	Content       string `json:"content"`
	TotalBytes    int64  `json:"total_bytes"`
	ReturnedBytes int    `json:"returned_bytes"`
	Truncated     bool   `json:"truncated"`
	Clamped       bool   `json:"clamped"`
}

func registerStreams(s *mcpsdk.Server) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "cg_stdout",
		Description: "Fetch captured stdout for a run. Defaults to the first 16 KiB; max_bytes caps the response (max 1 MiB), from=\"tail\" reads the final window, offset pages through head reads. Works for in-flight runs.",
	}, makeStreamHandler("stdout"))
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "cg_stderr",
		Description: "Fetch captured stderr for a run. Defaults to the first 16 KiB; max_bytes caps the response (max 1 MiB), from=\"tail\" reads the final window, offset pages through head reads. Works for in-flight runs.",
	}, makeStreamHandler("stderr"))
}

// makeStreamHandler binds the shared read implementation to a specific file
// name within a run's capture directory.
func makeStreamHandler(fileName string) func(context.Context, *mcpsdk.CallToolRequest, streamInput) (*mcpsdk.CallToolResult, streamOutput, error) {
	return func(_ context.Context, _ *mcpsdk.CallToolRequest, in streamInput) (*mcpsdk.CallToolResult, streamOutput, error) {
		return handleStream(fileName, in)
	}
}

func handleStream(fileName string, in streamInput) (*mcpsdk.CallToolResult, streamOutput, error) {
	from := in.From
	if from == "" {
		from = streamFromHead
	}
	if from != streamFromHead && from != streamFromTail {
		return nil, streamOutput{}, fmt.Errorf("invalid from: %q (want %q or %q)", in.From, streamFromHead, streamFromTail)
	}
	if in.Offset < 0 {
		return nil, streamOutput{}, fmt.Errorf("offset must be non-negative")
	}
	if in.MaxBytes < 0 {
		return nil, streamOutput{}, fmt.Errorf("max_bytes must be non-negative")
	}

	maxBytes := in.MaxBytes
	if maxBytes == 0 {
		maxBytes = defaultStreamBytes
	}
	clamped := false
	if maxBytes > maxStreamBytes {
		maxBytes = maxStreamBytes
		clamped = true
	}

	dir, err := cg.LookupRunDir(in.ID)
	if err != nil && !errors.Is(err, cg.ErrIncompleteRun) {
		return nil, streamOutput{}, mapLookupError(in.ID, err)
	}

	path := filepath.Join(dir, fileName)
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, streamOutput{}, fmt.Errorf("unknown run id: %s", in.ID)
		}
		return nil, streamOutput{}, fmt.Errorf("opening %s for %s: %w", fileName, in.ID, err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, streamOutput{}, fmt.Errorf("stat %s for %s: %w", fileName, in.ID, err)
	}
	total := info.Size()

	switch from {
	case streamFromHead:
		return readHead(f, total, in.Offset, maxBytes, clamped)
	default:
		return readTail(f, total, maxBytes, clamped)
	}
}

func readHead(f *os.File, total int64, offset int64, maxBytes int, clamped bool) (*mcpsdk.CallToolResult, streamOutput, error) {
	if offset >= total {
		return nil, streamOutput{TotalBytes: total, Clamped: clamped}, nil
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, streamOutput{}, fmt.Errorf("seeking: %w", err)
	}
	remaining := total - offset
	n := int64(maxBytes)
	if n > remaining {
		n = remaining
	}
	buf := make([]byte, n)
	read, err := io.ReadFull(f, buf)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return nil, streamOutput{}, fmt.Errorf("reading: %w", err)
	}
	return nil, streamOutput{
		Content:       string(buf[:read]),
		TotalBytes:    total,
		ReturnedBytes: read,
		Truncated:     offset+int64(read) < total,
		Clamped:       clamped,
	}, nil
}

func readTail(f *os.File, total int64, maxBytes int, clamped bool) (*mcpsdk.CallToolResult, streamOutput, error) {
	n := int64(maxBytes)
	if n > total {
		n = total
	}
	start := total - n
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return nil, streamOutput{}, fmt.Errorf("seeking: %w", err)
	}
	buf := make([]byte, n)
	read, err := io.ReadFull(f, buf)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return nil, streamOutput{}, fmt.Errorf("reading: %w", err)
	}
	return nil, streamOutput{
		Content:       string(buf[:read]),
		TotalBytes:    total,
		ReturnedBytes: read,
		Truncated:     int64(read) < total,
		Clamped:       clamped,
	}, nil
}
