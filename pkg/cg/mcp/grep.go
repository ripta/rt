package mcp

import (
	"context"
	"errors"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ripta/rt/pkg/cg/model"
)

// grepInput is the argument shape for `cg_grep`. Exactly one of text or pattern
// must be set: text is a fixed-string substring search, pattern is an RE2 regex.
type grepInput struct {
	ID              string `json:"id" jsonschema:"capture run ID"`
	Text            string `json:"text,omitempty" jsonschema:"fixed-string substring to match; mutually exclusive with pattern"`
	Pattern         string `json:"pattern,omitempty" jsonschema:"RE2 regular expression to match; mutually exclusive with text"`
	Streams         string `json:"streams,omitempty" jsonschema:"which streams to search: all (default), stdout, or stderr"`
	CaseInsensitive bool   `json:"case_insensitive,omitempty" jsonschema:"fold case when matching"`
	InvertMatch     bool   `json:"invert_match,omitempty" jsonschema:"return lines that do NOT match"`
	MaxMatches      int    `json:"max_matches,omitempty" jsonschema:"cap on returned matches; default 1000, max 10000"`
}

// grepMatch and grepOutput are the wire shapes for `cg_grep`. They alias the
// shared engine types so the MCP response and the `cg grep` CLI stay identical.
type grepMatch = model.GrepMatch
type grepOutput = model.GrepResult

func registerGrep(s *mcpsdk.Server) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "cg_grep",
		Description: "Search a run's captured output line by line and return matching lines with stream and 1-based line number. Supply exactly one of text (fixed string) or pattern (RE2 regex). Searches both streams by default; streams selects stdout or stderr. Supports case_insensitive and invert_match. Works for in-flight runs. Lines with invalid UTF-8 are base64-encoded and tagged content_encoding: \"base64\".",
	}, handleGrep)
}

func handleGrep(_ context.Context, _ *mcpsdk.CallToolRequest, in grepInput) (*mcpsdk.CallToolResult, grepOutput, error) {
	out, err := model.Grep(in.ID, model.GrepOptions{
		Text:            in.Text,
		Pattern:         in.Pattern,
		Streams:         in.Streams,
		CaseInsensitive: in.CaseInsensitive,
		InvertMatch:     in.InvertMatch,
		MaxMatches:      in.MaxMatches,
	})
	if err != nil {
		if errors.Is(err, model.ErrUnknownRunID) {
			return nil, grepOutput{}, mapLookupError(in.ID, err)
		}
		return nil, grepOutput{}, err
	}
	return nil, out, nil
}
