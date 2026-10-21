package mcp

import (
	"context"
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// legacyProtocolVersion is the newest protocol version that still allows a server to elicit
// while serving a request. The SDK fulfills returned InputRequests itself on it.
const legacyProtocolVersion = "2025-11-25"

// formPosition matches the position suffix cg_elicit_test appends when more than one form is in
// play. Capture 3 is the form's index within its round.
var formPosition = regexp.MustCompile(`\[round (\d+) of (\d+), form (\d+) of (\d+)\]$`)

type elicitProbeTest struct {
	Name            string
	ProtocolVersion string
	Args            map[string]any
	WantSupported   bool
	WantPrompts     int
	WantPositioned  bool
	WantResponses   []elicitResponse
	WantError       string
}

var elicitProbeTests = []elicitProbeTest{
	{
		Name:          "single form on multi round-trip protocol",
		Args:          map[string]any{"case": "checkbox"},
		WantSupported: true,
		WantPrompts:   1,
		WantResponses: []elicitResponse{
			{Round: 1, ID: "form1", Action: "accept"},
		},
	},
	{
		Name:           "batched forms over chained rounds",
		Args:           map[string]any{"case": "checkbox", "requests": 2, "rounds": 2},
		WantSupported:  true,
		WantPrompts:    4,
		WantPositioned: true,
		WantResponses: []elicitResponse{
			{Round: 1, ID: "form1", Action: "accept"},
			{Round: 1, ID: "form2", Action: "decline"},
			{Round: 2, ID: "form1", Action: "accept"},
			{Round: 2, ID: "form2", Action: "decline"},
		},
	},
	{
		Name:            "batched forms on legacy protocol",
		ProtocolVersion: legacyProtocolVersion,
		Args:            map[string]any{"case": "checkbox", "requests": 2},
		WantSupported:   true,
		WantPrompts:     2,
		WantPositioned:  true,
		WantResponses: []elicitResponse{
			{Round: 1, ID: "form1", Action: "accept"},
			{Round: 1, ID: "form2", Action: "decline"},
		},
	},
	{
		Name:            "chained rounds refused on legacy protocol",
		ProtocolVersion: legacyProtocolVersion,
		Args:            map[string]any{"case": "checkbox", "rounds": 2},
		WantError:       "rounds > 1 needs protocol 2026-07-28 or later, but the client negotiated 2025-11-25",
	},
	{
		Name:      "out-of-range counts report every violation",
		Args:      map[string]any{"requests": 11, "rounds": -1},
		WantError: "requests must be between 1 and 10, got 11; rounds must be between 1 and 10, got -1",
	},
}

// TestElicitProbe drives cg_elicit_test through a real SDK client. The client declines the second
// form of each round and accepts the rest with a checked box. It finds the second form through the
// position suffix, and the test asserts that suffix is present exactly when expected. A change to
// the suffix format therefore fails here instead of quietly turning every decline into an accept.
func TestElicitProbe(t *testing.T) {
	for _, test := range elicitProbeTests {
		t.Run(test.Name, func(t *testing.T) {
			var mu sync.Mutex
			prompts, positioned := 0, 0
			handler := func(_ context.Context, req *mcpsdk.ElicitRequest) (*mcpsdk.ElicitResult, error) {
				m := formPosition.FindStringSubmatch(req.Params.Message)

				mu.Lock()
				prompts++
				if m != nil {
					positioned++
				}
				mu.Unlock()

				if m != nil && m[3] == "2" {
					return &mcpsdk.ElicitResult{Action: "decline"}, nil
				}
				return &mcpsdk.ElicitResult{Action: "accept", Content: map[string]any{"always": true}}, nil
			}

			out := callElicitProbe(t, test.ProtocolVersion, &mcpsdk.CallToolParams{Name: "cg_elicit_test", Arguments: test.Args}, handler)

			wantVersion := test.ProtocolVersion
			if wantVersion == "" {
				wantVersion = multiRoundTripVersion
			}
			if out.ProtocolVersion != wantVersion {
				t.Errorf("ProtocolVersion = %q, want %q", out.ProtocolVersion, wantVersion)
			}
			if out.Error != test.WantError {
				t.Errorf("Error = %q, want %q", out.Error, test.WantError)
			}
			if out.Supported != test.WantSupported {
				t.Errorf("Supported = %v, want %v", out.Supported, test.WantSupported)
			}
			if prompts != test.WantPrompts {
				t.Errorf("prompts = %d, want %d", prompts, test.WantPrompts)
			}

			wantPositioned := 0
			if test.WantPositioned {
				wantPositioned = test.WantPrompts
			}
			if positioned != wantPositioned {
				t.Errorf("forms with a position suffix = %d, want %d", positioned, wantPositioned)
			}

			if len(out.Responses) != len(test.WantResponses) {
				t.Fatalf("got %d responses, want %d: %+v", len(out.Responses), len(test.WantResponses), out.Responses)
			}
			for i, want := range test.WantResponses {
				got := out.Responses[i]
				if got.Round != want.Round || got.ID != want.ID || got.Action != want.Action {
					t.Errorf("response %d = %+v, want round %d id %q action %q", i, got, want.Round, want.ID, want.Action)
				}

				switch want.Action {
				case "accept":
					if got.Content["always"] != true {
						t.Errorf("response %d content = %v, want always=true", i, got.Content)
					}
				default:
					if got.Content != nil {
						t.Errorf("response %d content = %v, want none", i, got.Content)
					}
				}
			}
		})
	}
}

// TestElicitProbeApprovalForm checks that the approval-form case sends the gate's own form: the
// position lead, every command, the two-token rule from a canonical path, and a line wider than
// any terminal. It also checks that a single request is raised to two so the lead renders.
func TestElicitProbeApprovalForm(t *testing.T) {
	var mu sync.Mutex
	var messages []string
	var schemas []string

	handler := func(_ context.Context, req *mcpsdk.ElicitRequest) (*mcpsdk.ElicitResult, error) {
		raw, err := json.Marshal(req.Params.RequestedSchema)
		if err != nil {
			t.Errorf("marshaling schema: %v", err)
		}

		mu.Lock()
		messages = append(messages, req.Params.Message)
		schemas = append(schemas, string(raw))
		mu.Unlock()

		return &mcpsdk.ElicitResult{Action: "accept", Content: map[string]any{"remember": false}}, nil
	}

	params := &mcpsdk.CallToolParams{Name: "cg_elicit_test", Arguments: map[string]any{"case": approvalFormCase, "commands": 4, "requests": 1}}
	out := callElicitProbe(t, "", params, handler)

	if out.Error != "" || !out.Supported {
		t.Fatalf("probe = %+v, want it supported with no error", out)
	}
	if len(messages) != 2 {
		t.Fatalf("forms = %d, want 2", len(messages))
	}

	for _, m := range messages {
		if got := len(listedCommands(m)); got != 4 {
			t.Errorf("form lists %d commands, want 4:\n%s", got, m)
		}
		if !strings.Contains(m, "Allow these 4 commands?") {
			t.Errorf("form lacks the plural wording:\n%s", m)
		}
		if !strings.Contains(m, "working directory: /"+strings.Repeat("0123456789", 12)) {
			t.Errorf("form lacks the wide line:\n%s", m)
		}
	}

	positions := []string{messages[0][:strings.Index(messages[0], "\n")], messages[1][:strings.Index(messages[1], "\n")]}
	slices.Sort(positions)
	if want := []string{"Approval 1 of 2", "Approval 2 of 2"}; !slices.Equal(positions, want) {
		t.Errorf("leads = %q, want %q", positions, want)
	}

	for _, wire := range []string{`[/usr/local/go/bin/go, test]`, `Remember this rule`, `.cg.yaml`} {
		if !strings.Contains(schemas[0], wire) {
			t.Errorf("schema lacks %q:\n%s", wire, schemas[0])
		}
	}
}

func TestElicitProbeApprovalFormRejectsBadCounts(t *testing.T) {
	params := &mcpsdk.CallToolParams{Name: "cg_elicit_test", Arguments: map[string]any{"case": approvalFormCase, "commands": maxProbeCommands + 1}}
	out := callElicitProbe(t, "", params, func(context.Context, *mcpsdk.ElicitRequest) (*mcpsdk.ElicitResult, error) {
		t.Errorf("client was prompted, want the count refused first")
		return &mcpsdk.ElicitResult{Action: "decline"}, nil
	})

	if want := "commands must be between 1 and 20, got 21"; out.Error != want {
		t.Errorf("Error = %q, want %q", out.Error, want)
	}
}

func TestElicitProbeIgnoresCommandsOutsideApprovalForm(t *testing.T) {
	params := &mcpsdk.CallToolParams{Name: "cg_elicit_test", Arguments: map[string]any{"case": "checkbox", "commands": 30}}
	out := callElicitProbe(t, "", params, func(context.Context, *mcpsdk.ElicitRequest) (*mcpsdk.ElicitResult, error) {
		return &mcpsdk.ElicitResult{Action: "accept", Content: map[string]any{"always": true}}, nil
	})

	if out.Error != "" || !out.Supported {
		t.Errorf("probe = %+v, want it supported with no error", out)
	}
}

type elicitStateTest struct {
	Name            string
	RequestState    string
	WantErrorPrefix string
}

// A client could send any request state on its first call. These pin how the probe treats a state
// it did not mint.
var elicitStateTests = []elicitStateTest{
	{
		Name:            "malformed state",
		RequestState:    "not json",
		WantErrorPrefix: "decoding request state: ",
	},
	{
		Name:            "round beyond the requested rounds",
		RequestState:    `{"round":5}`,
		WantErrorPrefix: "request state names round 5, outside 0..1",
	},
	{
		Name:            "negative round",
		RequestState:    `{"round":-1}`,
		WantErrorPrefix: "request state names round -1, outside 0..1",
	},
	{
		Name:            "round with no answers",
		RequestState:    `{"round":1}`,
		WantErrorPrefix: `round 1: no response for input request "form1"`,
	},
}

func TestElicitProbeRejectsForeignState(t *testing.T) {
	for _, test := range elicitStateTests {
		t.Run(test.Name, func(t *testing.T) {
			handler := func(context.Context, *mcpsdk.ElicitRequest) (*mcpsdk.ElicitResult, error) {
				t.Errorf("client was prompted, want the state refused before any prompt")
				return &mcpsdk.ElicitResult{Action: "decline"}, nil
			}

			params := &mcpsdk.CallToolParams{
				Name:         "cg_elicit_test",
				Arguments:    map[string]any{"case": "checkbox"},
				RequestState: test.RequestState,
			}
			out := callElicitProbe(t, "", params, handler)

			if out.Supported {
				t.Errorf("Supported = true, want false")
			}
			if out.Responses != nil {
				t.Errorf("Responses = %+v, want none", out.Responses)
			}
			if !strings.HasPrefix(out.Error, test.WantErrorPrefix) {
				t.Errorf("Error = %q, want prefix %q", out.Error, test.WantErrorPrefix)
			}
		})
	}
}

// TestElicitProbeRetryCap pins what the SDK client's retry cap looks like. Ten rounds is one more
// than the client completes, so the call fails in the client instead of returning a result.
func TestElicitProbeRetryCap(t *testing.T) {
	handler := func(context.Context, *mcpsdk.ElicitRequest) (*mcpsdk.ElicitResult, error) {
		return &mcpsdk.ElicitResult{Action: "accept", Content: map[string]any{"always": true}}, nil
	}

	cs := connectElicitProbe(t, "", handler)
	_, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      "cg_elicit_test",
		Arguments: map[string]any{"case": "checkbox", "rounds": maxElicitProbe},
	})
	if err == nil {
		t.Fatalf("CallTool succeeded, want the client's retry cap to fail it")
	}
	if want := "exceeded maximum retries"; !strings.Contains(err.Error(), want) {
		t.Errorf("CallTool error = %q, want it to contain %q", err, want)
	}
}

// TestElicitProbeWithoutCapability checks that a client that cannot elicit gets a clear refusal
// rather than an input-required result it cannot fulfill.
func TestElicitProbeWithoutCapability(t *testing.T) {
	out := callElicitProbe(t, "", &mcpsdk.CallToolParams{Name: "cg_elicit_test", Arguments: map[string]any{"case": "checkbox"}}, nil)

	if out.Supported {
		t.Errorf("Supported = true, want false")
	}
	if want := "client did not advertise the elicitation capability"; out.Error != want {
		t.Errorf("Error = %q, want %q", out.Error, want)
	}
}

// connectElicitProbe connects an SDK client to a fresh server at protocolVersion, or the SDK's
// latest when empty. A nil handler leaves the client without the elicitation capability.
func connectElicitProbe(t *testing.T, protocolVersion string, handler func(context.Context, *mcpsdk.ElicitRequest) (*mcpsdk.ElicitResult, error)) *mcpsdk.ClientSession {
	t.Helper()

	return connectGate(t, &gate{blindlyAllow: true}, protocolVersion, handler)
}

// connectGate connects an SDK client to a fresh server guarded by g, over an in-memory transport.
// protocolVersion is what the client proposes, or the SDK's latest when empty. A nil handler
// leaves the client without the elicitation capability.
func connectGate(t *testing.T, g *gate, protocolVersion string, handler func(context.Context, *mcpsdk.ElicitRequest) (*mcpsdk.ElicitResult, error)) *mcpsdk.ClientSession {
	t.Helper()

	return connectGateWith(t, g, protocolVersion, &mcpsdk.ClientOptions{ElicitationHandler: handler})
}

// connectGateWith is connectGate with the client's options spelled out, for tests that shape the
// client's capabilities or turn off its multi round-trip handling.
func connectGateWith(t *testing.T, g *gate, protocolVersion string, opts *mcpsdk.ClientOptions) *mcpsdk.ClientSession {
	t.Helper()

	ctx := context.Background()
	server := newServer("test", time.Now(), "SESS", g)

	serverTransport, clientTransport := mcpsdk.NewInMemoryTransports()
	ss, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	t.Cleanup(func() { _ = ss.Close() })

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test-client", Version: "0"}, opts)
	cs, err := client.Connect(ctx, clientTransport, &mcpsdk.ClientSessionOptions{ProtocolVersion: protocolVersion})
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })

	return cs
}

// callElicitProbe calls cg_elicit_test with params over a fresh connection and returns the decoded
// output.
func callElicitProbe(t *testing.T, protocolVersion string, params *mcpsdk.CallToolParams, handler func(context.Context, *mcpsdk.ElicitRequest) (*mcpsdk.ElicitResult, error)) elicitOutput {
	t.Helper()

	cs := connectElicitProbe(t, protocolVersion, handler)

	res, err := cs.CallTool(context.Background(), params)
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool error: %+v", res.Content)
	}

	data, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshaling structured content: %v", err)
	}

	var out elicitOutput
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshaling elicit output: %v", err)
	}

	return out
}
