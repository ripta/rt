package mcp

import (
	"context"
	"errors"
	"fmt"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ripta/rt/pkg/cg"
)

// note is the wire shape for a single note. It aliases the store's record type
// so the MCP response and the `cg note` CLI stay identical.
type note = cg.Note

// noteAddInput is the argument shape for `cg_note_add`. Message is required; the
// store enforces the message and keys size bounds.
type noteAddInput struct {
	Message string            `json:"message" jsonschema:"note message body; required and non-empty"`
	Keys    map[string]string `json:"keys,omitempty" jsonschema:"optional string tags for later filtering, e.g. {\"run\": \"4KQ2ZP\"}"`
}

// noteListInput is the argument shape for `cg_note_list`.
type noteListInput struct {
	Key   string  `json:"key,omitempty" jsonschema:"keep only notes carrying this key"`
	Value *string `json:"value,omitempty" jsonschema:"with key, additionally require this exact value; absent means match on key existence"`
	Limit int     `json:"limit,omitempty" jsonschema:"maximum number of notes to return; default 20, max 1000"`
}

// noteListOutput is the result shape for `cg_note_list` and `cg_note_grep`. Both
// return whole notes newest-first.
type noteListOutput struct {
	Notes []note `json:"notes"`
}

// noteDeleteInput is the argument shape for `cg_note_delete`.
type noteDeleteInput struct {
	ID string `json:"id" jsonschema:"note ID to delete"`
}

// noteDeleteOutput is the result shape for `cg_note_delete`.
type noteDeleteOutput struct {
	ID      string `json:"id"`
	Deleted bool   `json:"deleted"`
}

// noteGrepInput is the argument shape for `cg_note_grep`. Exactly one of text or
// pattern must be set: text is a fixed-string substring search, pattern is an
// RE2 regex.
type noteGrepInput struct {
	Text            string `json:"text,omitempty" jsonschema:"fixed-string substring to match; mutually exclusive with pattern"`
	Pattern         string `json:"pattern,omitempty" jsonschema:"RE2 regular expression to match; mutually exclusive with text"`
	CaseInsensitive bool   `json:"case_insensitive,omitempty" jsonschema:"fold case when matching"`
}

func registerNotes(s *mcpsdk.Server) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "cg_note_add",
		Description: "Record a free-form note that outlives a single tool call. The message is required. Optional keys are string tags for later filtering, e.g. {\"run\": \"4KQ2ZP\"}. Returns the stored note with its ID. Notes are visible to the `cg note` shell subcommands and vice versa.",
	}, handleNoteAdd)
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "cg_note_list",
		Description: "List notes newest-first by creation time. Optional key filters to notes carrying that key; with value, requires that exact value. Limit defaults to 20 and caps at 1000.",
	}, handleNoteList)
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "cg_note_delete",
		Description: "Delete a note by ID. Returns an error if no note with that ID exists.",
	}, handleNoteDelete)
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "cg_note_grep",
		Description: "Search note message bodies and return whole matching notes newest-first. Supply exactly one of text (fixed string) or pattern (RE2 regex). Supports case_insensitive. Key filtering lives on cg_note_list, not here.",
	}, handleNoteGrep)
}

func handleNoteAdd(_ context.Context, _ *mcpsdk.CallToolRequest, in noteAddInput) (*mcpsdk.CallToolResult, note, error) {
	n, err := cg.AddNote(in.Message, in.Keys)
	if err != nil {
		return nil, note{}, err
	}
	return nil, *n, nil
}

func handleNoteList(_ context.Context, _ *mcpsdk.CallToolRequest, in noteListInput) (*mcpsdk.CallToolResult, noteListOutput, error) {
	limit := in.Limit
	if limit <= 0 {
		limit = defaultListLimit
	}
	if limit > maxListLimit {
		limit = maxListLimit
	}

	opts := cg.ListNoteOptions{Key: in.Key, Limit: limit}
	if in.Value != nil {
		opts.HasValue = true
		opts.Value = *in.Value
	}

	notes, err := cg.ListNotes(opts)
	if err != nil {
		return nil, noteListOutput{}, err
	}
	if notes == nil {
		notes = []note{}
	}
	return nil, noteListOutput{Notes: notes}, nil
}

func handleNoteDelete(_ context.Context, _ *mcpsdk.CallToolRequest, in noteDeleteInput) (*mcpsdk.CallToolResult, noteDeleteOutput, error) {
	// cg.DeleteNote joins the ID into a path with no sanitization, so validate
	// the untrusted ID before it reaches the filesystem.
	if !cg.IsValidRunID(in.ID) {
		return nil, noteDeleteOutput{}, fmt.Errorf("invalid note id: %s", in.ID)
	}

	if err := cg.DeleteNote(in.ID); err != nil {
		if errors.Is(err, cg.ErrUnknownNoteID) {
			return nil, noteDeleteOutput{}, fmt.Errorf("unknown note id: %s", in.ID)
		}
		return nil, noteDeleteOutput{}, err
	}
	return nil, noteDeleteOutput{ID: in.ID, Deleted: true}, nil
}

func handleNoteGrep(_ context.Context, _ *mcpsdk.CallToolRequest, in noteGrepInput) (*mcpsdk.CallToolResult, noteListOutput, error) {
	notes, err := cg.GrepNotes(cg.GrepNoteOptions{
		Text:            in.Text,
		Pattern:         in.Pattern,
		CaseInsensitive: in.CaseInsensitive,
	})
	if err != nil {
		return nil, noteListOutput{}, err
	}
	if notes == nil {
		notes = []note{}
	}
	return nil, noteListOutput{Notes: notes}, nil
}
