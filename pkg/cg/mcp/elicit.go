package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// maxElicitProbe bounds both requests per round and rounds per call. The SDK client gives up
// after ten tool calls, so ten rounds is one more than it can complete, which lets the probe
// show what hitting that cap looks like.
const maxElicitProbe = 10

// elicitInput is the argument shape for `cg_elicit_test`, a diagnostic that
// exercises MCP elicitation against the connected client. The case selects
// which kind of form schema to send so each field type can be inspected on its
// own.
//
// Requests and rounds shape the multi round-trip exchange: each round returns that many
// elicitations together in one input-required result, and the call completes after the last
// round is answered.
type elicitInput struct {
	Case     string `json:"case,omitempty" jsonschema:"which form to send: text, text_default, enum_single, enum_single_titled, enum_multi, enum_multi_titled, checkbox, number, format, all (default all)"`
	Message  string `json:"message,omitempty" jsonschema:"prompt to show the user; a per-case default is used when empty"`
	Requests int    `json:"requests,omitempty" jsonschema:"elicitations sent together in each round (1-10, default 1)"`
	Rounds   int    `json:"rounds,omitempty" jsonschema:"input-required rounds before the call completes (1-10, default 1); more than one needs protocol 2026-07-28 or later"`
}

// elicitOutput reports whether elicitation is supported and, when it is, every
// response the client returned across all rounds, plus the schema that was sent
// so the request and response can be compared.
type elicitOutput struct {
	Supported       bool             `json:"supported"`
	ProtocolVersion string           `json:"protocol_version,omitempty"`
	Case            string           `json:"case"`
	Schema          map[string]any   `json:"schema,omitempty"`
	Responses       []elicitResponse `json:"responses,omitempty"`
	Error           string           `json:"error,omitempty"`
}

// elicitResponse is one answered elicitation. ID is the input request key, unique within its
// round.
type elicitResponse struct {
	Round   int            `json:"round"`
	ID      string         `json:"id"`
	Action  string         `json:"action"`
	Content map[string]any `json:"content,omitempty"`
}

// elicitState is the probe's RequestState, carrying the last round sent and the responses
// collected so far. It is unsigned on purpose: the probe grants nothing, so a forged state can
// only misreport the probe's own results.
type elicitState struct {
	Round     int              `json:"round"`
	Responses []elicitResponse `json:"responses,omitempty"`
}

func registerElicitTest(s *mcpsdk.Server) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        "cg_elicit_test",
		Description: "Diagnostic: send MCP elicitation forms to the client to verify support and inspect how each field type renders and round-trips. Use the case argument to pick a field type, requests to send several forms at once, and rounds to chain several input-required rounds.",
	}, handleElicitTest)
}

func handleElicitTest(_ context.Context, req *mcpsdk.CallToolRequest, in elicitInput) (*mcpsdk.CallToolResult, elicitOutput, error) {
	kase := in.Case
	if kase == "" {
		kase = "all"
	}

	out := elicitOutput{Case: kase, ProtocolVersion: negotiatedVersion(req)}

	requests, rounds, err := elicitCounts(in)
	if err != nil {
		out.Error = err.Error()
		return nil, out, nil
	}

	schema, msg, err := elicitSchema(kase)
	if err != nil {
		out.Error = err.Error()
		return nil, out, nil
	}
	if in.Message != "" {
		msg = in.Message
	}
	out.Schema = schema

	if !elicitationAvailable(req) {
		out.Error = "client did not advertise the elicitation capability"
		return nil, out, nil
	}
	if rounds > 1 && !multiRoundTripAvailable(req) {
		out.Error = fmt.Sprintf("rounds > 1 needs protocol %s or later, but the client negotiated %s", multiRoundTripVersion, out.ProtocolVersion)
		return nil, out, nil
	}

	var state elicitState
	if req.Params.RequestState != "" {
		if err := json.Unmarshal([]byte(req.Params.RequestState), &state); err != nil {
			out.Error = fmt.Sprintf("decoding request state: %v", err)
			return nil, out, nil
		}
	}
	if state.Round < 0 || state.Round > rounds {
		out.Error = fmt.Sprintf("request state names round %d, outside 0..%d", state.Round, rounds)
		return nil, out, nil
	}

	if state.Round > 0 {
		collected, err := collectElicitResponses(state.Round, requests, req.Params.InputResponses)
		if err != nil {
			out.Error = err.Error()
			return nil, out, nil
		}
		state.Responses = append(state.Responses, collected...)
	}

	if state.Round >= rounds {
		out.Supported = true
		out.Responses = state.Responses
		return nil, out, nil
	}

	state.Round++
	encoded, err := json.Marshal(state)
	if err != nil {
		return nil, elicitOutput{}, fmt.Errorf("encoding request state: %w", err)
	}

	return &mcpsdk.CallToolResult{
		InputRequests: elicitRequests(state.Round, rounds, requests, msg, schema),
		RequestState:  string(encoded),
	}, elicitOutput{}, nil
}

// elicitCounts applies the defaults for requests and rounds and rejects values outside
// 1..maxElicitProbe, reporting every violation at once.
func elicitCounts(in elicitInput) (int, int, error) {
	requests, rounds := in.Requests, in.Rounds
	if requests == 0 {
		requests = 1
	}
	if rounds == 0 {
		rounds = 1
	}

	var violations []string
	if requests < 1 || requests > maxElicitProbe {
		violations = append(violations, fmt.Sprintf("requests must be between 1 and %d, got %d", maxElicitProbe, requests))
	}
	if rounds < 1 || rounds > maxElicitProbe {
		violations = append(violations, fmt.Sprintf("rounds must be between 1 and %d, got %d", maxElicitProbe, rounds))
	}
	if len(violations) > 0 {
		return 0, 0, fmt.Errorf("%s", strings.Join(violations, "; "))
	}

	return requests, rounds, nil
}

// elicitRequests builds one round's input requests. Each gets a position suffix whenever there
// is more than one form in play, so the user can tell the forms apart.
func elicitRequests(round, rounds, requests int, msg string, schema map[string]any) mcpsdk.InputRequestMap {
	irs := make(mcpsdk.InputRequestMap, requests)
	for i := 1; i <= requests; i++ {
		m := msg
		if requests > 1 || rounds > 1 {
			m = fmt.Sprintf("%s\n\n[round %d of %d, form %d of %d]", msg, round, rounds, i, requests)
		}

		irs[elicitRequestID(i)] = &mcpsdk.ElicitParams{
			Message:         m,
			RequestedSchema: schema,
		}
	}

	return irs
}

// collectElicitResponses reads one round's answers in request order. A missing or non-elicit
// response is an error, since it means the client did not answer what was asked.
func collectElicitResponses(round, requests int, responses mcpsdk.InputResponseMap) ([]elicitResponse, error) {
	out := make([]elicitResponse, 0, requests)
	for i := 1; i <= requests; i++ {
		id := elicitRequestID(i)
		resp, ok := responses[id]
		if !ok {
			return nil, fmt.Errorf("round %d: no response for input request %q", round, id)
		}

		res, ok := resp.(*mcpsdk.ElicitResult)
		if !ok {
			return nil, fmt.Errorf("round %d: input request %q answered with %T, want an elicitation result", round, id, resp)
		}

		out = append(out, elicitResponse{Round: round, ID: id, Action: res.Action, Content: res.Content})
	}

	return out, nil
}

func elicitRequestID(i int) string {
	return fmt.Sprintf("form%d", i)
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

// multiRoundTripVersion is the first MCP protocol version on which the client itself fulfills
// InputRequests and retries the call. The cg_elicit_test probe needs it to chain more than one
// round.
//
// On older versions the SDK's receiving middleware fulfills returned InputRequests itself with a
// classic elicitation and reinvokes the handler exactly once. One round therefore works on every
// version, but a second round only works from this version on.
//
// Protocol versions are ISO dates, so string comparison orders them.
const multiRoundTripVersion = "2026-07-28"

// negotiatedVersion reports the protocol version the session negotiated, or empty when the
// request carries no session. The cg_elicit_test probe echoes it in its output.
func negotiatedVersion(req *mcpsdk.CallToolRequest) string {
	if req == nil || req.Session == nil {
		return ""
	}

	params := req.Session.InitializeParams()
	if params == nil {
		return ""
	}

	return params.ProtocolVersion
}

// multiRoundTripAvailable reports whether the probe may chain more than one round, which needs a
// client that fulfills InputRequests and retries itself.
func multiRoundTripAvailable(req *mcpsdk.CallToolRequest) bool {
	return negotiatedVersion(req) >= multiRoundTripVersion
}
