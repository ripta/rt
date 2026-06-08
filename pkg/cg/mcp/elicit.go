package mcp

import (
	"context"
	"fmt"
	"sort"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// elicitInput is the argument shape for `cg_elicit_test`, a diagnostic that
// exercises MCP elicitation against the connected client. The case selects
// which kind of form schema to send so each field type can be inspected on its
// own.
type elicitInput struct {
	Case    string `json:"case,omitempty" jsonschema:"which form to send: text, text_default, enum_single, enum_single_titled, enum_multi, enum_multi_titled, checkbox, number, format, all (default all)"`
	Message string `json:"message,omitempty" jsonschema:"prompt to show the user; a per-case default is used when empty"`
}

// elicitOutput reports whether elicitation is supported and, when it is, the
// user's action and submitted form content, plus the schema that was sent so
// the request and response can be compared.
type elicitOutput struct {
	Supported bool           `json:"supported"`
	Case      string         `json:"case"`
	Schema    map[string]any `json:"schema,omitempty"`
	Action    string         `json:"action,omitempty"`
	Content   map[string]any `json:"content,omitempty"`
	Error     string         `json:"error,omitempty"`
}

func registerElicitTest(s *mcpsdk.Server) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "cg_elicit_test",
		Description: "Diagnostic: send an MCP elicitation form to the client to verify support and inspect how each field type renders and round-trips. Use the case argument to pick a field type.",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, in elicitInput) (*mcpsdk.CallToolResult, elicitOutput, error) {
		kase := in.Case
		if kase == "" {
			kase = "all"
		}
		schema, msg, err := elicitSchema(kase)
		if err != nil {
			return nil, elicitOutput{Case: kase, Error: err.Error()}, nil
		}
		if in.Message != "" {
			msg = in.Message
		}
		res, err := req.Session.Elicit(ctx, &mcpsdk.ElicitParams{
			Message:         msg,
			RequestedSchema: schema,
		})
		if err != nil {
			return nil, elicitOutput{Supported: false, Case: kase, Schema: schema, Error: err.Error()}, nil
		}
		return nil, elicitOutput{
			Supported: true,
			Case:      kase,
			Schema:    schema,
			Action:    res.Action,
			Content:   res.Content,
		}, nil
	})
}

func obj(props map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		req := make([]any, len(required))
		for i, r := range required {
			req[i] = r
		}
		s["required"] = req
	}
	return s
}

// elicitSchema returns the requested-form schema and default prompt for a case.
func elicitSchema(kase string) (map[string]any, string, error) {
	switch kase {
	case "text":
		return obj(map[string]any{
			"answer": map[string]any{
				"type": "string", "title": "Answer", "description": "any short text",
				"minLength": 1, "maxLength": 80,
			},
		}, "answer"), "Plain text entry: type anything and submit.", nil

	case "text_default":
		return obj(map[string]any{
			"answer": map[string]any{
				"type": "string", "title": "Answer", "description": "edit or accept the prefilled value",
				"default": "prefilled default",
			},
		}), "Text entry with a default: submit as-is or edit first.", nil

	case "enum_single":
		return obj(map[string]any{
			"decision": map[string]any{
				"type": "string", "title": "Decision", "description": "pick one",
				"enum": []any{"accept", "decline", "always accept", "always decline"},
			},
		}, "decision"), "Single-choice enum: pick one option.", nil

	case "enum_single_titled":
		return obj(map[string]any{
			"decision": map[string]any{
				"type": "string", "title": "Decision", "description": "pick one (values carry display titles)",
				"oneOf": []any{
					map[string]any{"const": "accept", "title": "Accept once"},
					map[string]any{"const": "decline", "title": "Decline once"},
					map[string]any{"const": "always_accept", "title": "Always accept"},
					map[string]any{"const": "always_decline", "title": "Always decline"},
				},
			},
		}, "decision"), "Single-choice titled enum: labels differ from stored values.", nil

	case "enum_multi":
		return obj(map[string]any{
			"streams": map[string]any{
				"type": "array", "title": "Streams", "description": "select zero or more",
				"items": map[string]any{"type": "string", "enum": []any{"stdout", "stderr", "meta"}},
			},
		}), "Multi-choice enum: select any number of options.", nil

	case "enum_multi_titled":
		return obj(map[string]any{
			"steps": map[string]any{
				"type": "array", "title": "Steps", "description": "select any (titled)",
				"items": map[string]any{
					"anyOf": []any{
						map[string]any{"const": "build", "title": "Build"},
						map[string]any{"const": "test", "title": "Test"},
						map[string]any{"const": "lint", "title": "Lint"},
					},
				},
			},
		}), "Multi-choice titled enum: select any number, labels differ from values.", nil

	case "checkbox":
		return obj(map[string]any{
			"always": map[string]any{
				"type": "boolean", "title": "Always allow",
				"description": "checked = always, unchecked = one time",
				"default":     false,
			},
		}), "Checkbox: check to remember (always), leave unchecked for one time.", nil

	case "number":
		return obj(map[string]any{
			"count": map[string]any{
				"type": "integer", "title": "Count", "description": "1 to 10",
				"minimum": 1, "maximum": 10, "default": 3,
			},
		}, "count"), "Number entry: integer between 1 and 10.", nil

	case "format":
		return obj(map[string]any{
			"email": map[string]any{"type": "string", "title": "Email", "format": "email", "description": "an email address"},
			"when":  map[string]any{"type": "string", "title": "Date", "format": "date", "description": "a date"},
		}), "Formatted strings: email and date inputs.", nil

	case "all":
		return obj(map[string]any{
			"answer": map[string]any{"type": "string", "title": "Answer", "default": "prefilled", "description": "text with default"},
			"decision": map[string]any{
				"type": "string", "title": "Decision", "description": "single choice",
				"oneOf": []any{
					map[string]any{"const": "accept", "title": "Accept once"},
					map[string]any{"const": "always_accept", "title": "Always accept"},
					map[string]any{"const": "decline", "title": "Decline"},
				},
			},
			"count":  map[string]any{"type": "integer", "title": "Count", "minimum": 1, "maximum": 10, "default": 1},
			"always": map[string]any{"type": "boolean", "title": "Remember choice", "default": false},
		}, "decision"), "Combined form: text+default, single enum, number, and checkbox together.", nil

	default:
		return nil, "", fmt.Errorf("unknown case %q (want one of: %s)", kase, strings.Join(elicitCases(), ", "))
	}
}

func elicitCases() []string {
	cs := []string{
		"text", "text_default", "enum_single", "enum_single_titled",
		"enum_multi", "enum_multi_titled", "checkbox", "number", "format", "all",
	}
	sort.Strings(cs)
	return cs
}
