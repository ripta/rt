package mcp

import (
	"os"
	"strings"
	"testing"

	jsonschema "github.com/google/jsonschema-go/jsonschema"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	secretToken  = "ghp_aB3aB3aB3aB3aB3aB3aB3aB3aB3aB3aB3aB3"
	secretHeader = "Authorization: Bearer abcdef0123456789abcdef"
)

// ruleDescriptionOf reads the rule field's description from one form of a pending result.
func ruleDescriptionOf(t *testing.T, pending *mcpsdk.CallToolResult, id string) string {
	t.Helper()

	params, ok := pending.InputRequests[id].(*mcpsdk.ElicitParams)
	if !ok {
		t.Fatalf("input request %q = %T, want an elicitation", id, pending.InputRequests[id])
	}

	schema, ok := params.RequestedSchema.(*jsonschema.Schema)
	if !ok {
		t.Fatalf("schema = %T, want *jsonschema.Schema", params.RequestedSchema)
	}

	return schema.Properties["rule"].Description
}

func TestPrefillNamesTheCredentialPosition(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	g := newTestGateAt(t, t.TempDir(), "version: 1\n", false)
	pending := firstRound(t, g, testCall([]string{"echo", "hi", secretToken}))

	desc := ruleDescriptionOf(t, pending, "approval-1")
	if !strings.Contains(desc, "credential at token 3") {
		t.Errorf("description does not name the position: %s", desc)
	}
	if strings.Contains(desc, secretToken) {
		t.Errorf("description echoes the token: %s", desc)
	}
}

func TestFieldTextNeverSaysARuleIsClean(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	g := newTestGateAt(t, t.TempDir(), "version: 1\n", false)

	for _, argv := range [][]string{{"echo", "hi"}, {"echo", "hi", secretToken}} {
		pending := firstRound(t, g, testCall(argv))
		desc := strings.ToLower(ruleDescriptionOf(t, pending, "approval-1"))

		for _, bad := range []string{"no secret", "free of", "clean", "safe", "no credential", "not a credential"} {
			if strings.Contains(desc, bad) {
				t.Errorf("description for %q says %q: %s", argv, bad, desc)
			}
		}
	}
}

type credentialAnswerTest struct {
	Name   string
	Answer map[string]any

	WantNote []string
}

var credentialAnswerTests = []credentialAnswerTest{
	{
		Name:     "an unchanged field",
		Answer:   map[string]any{"remember": true},
		WantNote: []string{"credential at token 3", "nothing was saved", "Uncheck remember", "write the rule by hand", "trim the rule to the tokens before token 3", "widens the rule"},
	},
	{
		Name:     "the box ticked",
		Answer:   map[string]any{"remember": true, "allow_extra_args": true},
		WantNote: []string{"credential at token 3", "Uncheck remember"},
	},
	{
		Name:     "an edited field that keeps the token",
		Answer:   map[string]any{"remember": true, "rule": "[echo, hi, " + secretToken + "]"},
		WantNote: []string{"credential at token 3"},
	},
	{
		Name:     "a header value",
		Answer:   map[string]any{"remember": true, "rule": "[echo, \"" + secretHeader + "\"]"},
		WantNote: []string{"credential at token 2"},
	},
}

func TestRememberRefusesACredentialInTheSecondRound(t *testing.T) {
	for _, test := range credentialAnswerTests {
		t.Run(test.Name, func(t *testing.T) {
			t.Setenv("TMPDIR", t.TempDir())

			root := t.TempDir()
			g := newTestGateAt(t, root, "version: 1\n", false)

			call := multiRound(testCall([]string{"echo", "hi", secretToken}))
			pending := firstRound(t, g, call)

			second, err := g.check(retryCall(call, pending, mcpsdk.InputResponseMap{"approval-1": accept(test.Answer)}))
			if err != nil || second.Pending == nil {
				t.Fatalf("retry = %+v, %v; want a second round", second, err)
			}

			note := ruleDescriptionOf(t, second.Pending, "approval-1")
			for _, want := range test.WantNote {
				if !strings.Contains(note, want) {
					t.Errorf("note lacks %q:\n%s", want, note)
				}
			}
			for _, secret := range []string{secretToken, secretHeader, "abcdef0123456789abcdef"} {
				if strings.Contains(note, secret) {
					t.Errorf("note echoes %q:\n%s", secret, note)
				}
			}

			if i, j, k := strings.Index(note, "Uncheck remember"), strings.Index(note, "write the rule by hand"), strings.Index(note, "trim the rule"); i > j || (k >= 0 && j > k) {
				t.Errorf("the note lists the exits out of order:\n%s", note)
			}

			if file, _ := readProjectIfPresent(root); strings.Contains(file, "echo") {
				t.Errorf("a rule was written:\n%s", file)
			}

			final, err := g.check(retryCall(call, second.Pending, mcpsdk.InputResponseMap{
				"approval-1": accept(map[string]any{"remember": true, "rule": "[echo, hi]"}),
			}))
			if err != nil || final.Pending != nil || final.Warning != "" {
				t.Fatalf("a trim before the token = %+v, %v; want a clean run", final, err)
			}

			file, _ := readProjectIfPresent(root)
			if !strings.Contains(file, "prefix: [echo, hi]") || strings.Contains(file, secretToken) {
				t.Errorf("project file after the trim:\n%s", file)
			}
		})
	}
}

func TestCredentialAsFirstTokenOffersNoTrim(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	g := newTestGateAt(t, t.TempDir(), "version: 1\n", false)

	call := multiRound(testCall([]string{"echo", "hi"}))
	pending := firstRound(t, g, call)

	second, err := g.check(retryCall(call, pending, mcpsdk.InputResponseMap{
		"approval-1": accept(map[string]any{"remember": true, "rule": "[" + secretToken + "]"}),
	}))
	if err != nil || second.Pending == nil {
		t.Fatalf("retry = %+v, %v; want a second round", second, err)
	}

	note := ruleDescriptionOf(t, second.Pending, "approval-1")
	if strings.Contains(note, "trim the rule") || !strings.Contains(note, "no trim removes it") {
		t.Errorf("note should offer no trim:\n%s", note)
	}
}

func TestCredentialAndWrapperRefusalsOfferOnlyEditsThatPassBoth(t *testing.T) {
	w := newWrapperEnv(t)
	rules := "version: 1\nallow:\n  - wrapper: [" + w.Wrap + ", -n, \"19\"]\n  - prefix: [" + w.Tool + "]\n"
	argv := []string{"wrap", "-n", "10", "tool", secretToken}

	for _, rule := range []string{"", "[WRAP, -n, \"10\", tool, " + secretToken + "]"} {
		g, call, pending, root := wrappedRound(t, w, rules, argv)
		call = multiRound(call)

		answer := map[string]any{"remember": true}
		if rule != "" {
			answer["rule"] = strings.ReplaceAll(rule, "WRAP", w.Wrap)
		}

		second, err := g.check(retryCall(call, pending, mcpsdk.InputResponseMap{"approval-1": accept(answer)}))
		if err != nil || second.Pending == nil {
			t.Fatalf("retry = %+v, %v; want a second round", second, err)
		}

		note := ruleDescriptionOf(t, second.Pending, "approval-1")
		for _, want := range []string{"credential at token", "Uncheck remember", "write the rule by hand", "a trimmed rule is refused too"} {
			if !strings.Contains(note, want) {
				t.Errorf("note lacks %q:\n%s", want, note)
			}
		}
		for _, bad := range []string{"trim the rule to", "respelling the first token", "ticking allow extra"} {
			if strings.Contains(note, bad) {
				t.Errorf("note offers %q, which fails one of the refusals:\n%s", bad, note)
			}
		}
		if strings.Contains(note, secretToken) {
			t.Errorf("note echoes the token:\n%s", note)
		}

		if file, _ := readProjectIfPresent(root); strings.Contains(file, "exact:") {
			t.Errorf("a rule was written:\n%s", file)
		}
	}
}

func TestMismatchOnAWrapperHeadFirstTokenOffersNoTrimOrBox(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	g := newTestGateAt(t, t.TempDir(), "version: 1\nallow:\n  - wrapper: [nice, -n, \"19\"]\n", false)

	call := multiRound(testCall(echoHi))
	pending := firstRound(t, g, call)

	second, err := g.check(retryCall(call, pending, mcpsdk.InputResponseMap{
		"approval-1": accept(map[string]any{"remember": true, "rule": "[nice, hi]"}),
	}))
	if err != nil || second.Pending == nil {
		t.Fatalf("retry = %+v, %v; want a second round", second, err)
	}

	note := ruleDescriptionOf(t, second.Pending, "approval-1")
	if !strings.Contains(note, "does not match the command") || !strings.Contains(note, "respelling the first token and unchecking remember") {
		t.Errorf("note should be exact-only:\n%s", note)
	}
	for _, bad := range []string{"trimming it to a leading part", "ticking allow extra arguments"} {
		if strings.Contains(note, bad) {
			t.Errorf("note offers %q for a first token that names a wrapper program:\n%s", bad, note)
		}
	}
}

func TestMismatchNoteRedactsACredentialInTheCommand(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	g := newTestGateAt(t, t.TempDir(), "version: 1\n", false)

	call := multiRound(testCall([]string{"echo", "hi", secretToken}))
	pending := firstRound(t, g, call)

	second, err := g.check(retryCall(call, pending, mcpsdk.InputResponseMap{
		"approval-1": accept(map[string]any{"remember": true, "rule": "[echo, bye]"}),
	}))
	if err != nil || second.Pending == nil {
		t.Fatalf("retry = %+v, %v; want a second round", second, err)
	}

	note := ruleDescriptionOf(t, second.Pending, "approval-1")
	if strings.Contains(note, secretToken) || !strings.Contains(note, "does not match the command") || !strings.Contains(note, "<credential>") {
		t.Errorf("mismatch note:\n%s", note)
	}
}

func TestUnparseableFieldWithABareTokenDoesNotEchoItsStart(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	g := newTestGateAt(t, t.TempDir(), "version: 1\n", false)

	call := multiRound(testCall(echoHi))
	pending := firstRound(t, g, call)

	second, err := g.check(retryCall(call, pending, mcpsdk.InputResponseMap{
		"approval-1": accept(map[string]any{"remember": true, "rule": secretToken}),
	}))
	if err != nil || second.Pending == nil {
		t.Fatalf("retry = %+v, %v; want a second round", second, err)
	}

	note := ruleDescriptionOf(t, second.Pending, "approval-1")
	if strings.Contains(note, "ghp_") || !strings.Contains(note, "was not read") {
		t.Errorf("note echoes the start of the token or is missing:\n%s", note)
	}
}

func TestLegacyWarningForABareTokenFieldDoesNotEchoItsStart(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	g := newTestGateAt(t, t.TempDir(), "version: 1\n", false)

	script := &editScript{answers: []*mcpsdk.ElicitResult{accept(map[string]any{"remember": true, "rule": secretToken})}}
	cs := connectGate(t, g, protocolLegacy, script.handle)

	out, refusal, err := callGated(t, cs, "cg_run", map[string]any{"command": []string{"echo", "hi"}})
	if err != nil || refusal != "" {
		t.Fatalf("cg_run: err=%v refusal=%q", err, refusal)
	}

	warning, _ := out["remember_warning"].(string)
	if strings.Contains(warning, "ghp_") || !strings.Contains(warning, "was not read") {
		t.Errorf("remember_warning = %q", warning)
	}
}

func TestUnparseableFieldDoesNotEchoACredential(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	g := newTestGateAt(t, t.TempDir(), "version: 1\n", false)

	call := multiRound(testCall(echoHi))
	pending := firstRound(t, g, call)

	second, err := g.check(retryCall(call, pending, mcpsdk.InputResponseMap{
		"approval-1": accept(map[string]any{"remember": true, "rule": secretHeader}),
	}))
	if err != nil || second.Pending == nil {
		t.Fatalf("retry = %+v, %v; want a second round", second, err)
	}

	note := ruleDescriptionOf(t, second.Pending, "approval-1")
	if strings.Contains(note, "abcdef0123456789abcdef") || !strings.Contains(note, "was not read") {
		t.Errorf("unparseable note:\n%s", note)
	}
}

func TestUnsavedRuleRedactsACredential(t *testing.T) {
	got := unsavedRule("p.yaml", rememberRule{Tokens: []string{"echo", secretToken}}, "changed")
	if strings.Contains(got, secretToken) || !strings.Contains(got, "<credential>") {
		t.Errorf("unsavedRule = %q", got)
	}
}

func TestEndToEndCredentialRememberOnANewClient(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	root := t.TempDir()
	g := newTestGateAt(t, root, "version: 1\n", false)

	script := &editScript{answers: []*mcpsdk.ElicitResult{
		accept(map[string]any{"remember": true}),
		accept(map[string]any{"remember": false}),
	}}
	cs := connectGate(t, g, protocolMultiRoundTrip, script.handle)

	out, refusal, err := callGated(t, cs, "cg_run", map[string]any{"command": []string{"echo", "hi", secretToken}})
	if err != nil || refusal != "" {
		t.Fatalf("cg_run: err=%v refusal=%q", err, refusal)
	}
	if code, ok := out["exit_code"].(float64); !ok || code != 0 {
		t.Errorf("exit_code = %v, want 0", out["exit_code"])
	}
	if warning, has := out["remember_warning"]; has {
		t.Errorf("remember_warning = %v, want none after remember was unchecked", warning)
	}

	if len(script.shown) != 2 || !strings.Contains(script.schemas[1], "credential at token 3") {
		t.Fatalf("forms shown = %d, schemas = %v; want a second round that names the position", len(script.shown), script.schemas)
	}

	if _, err := os.Stat(projectFile(root)); err != nil {
		t.Fatal(err)
	}
	if file, _ := readProjectIfPresent(root); strings.Contains(file, "echo") {
		t.Errorf("a rule was written:\n%s", file)
	}
}

func TestEndToEndCredentialRememberOnAnOldClient(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	root := t.TempDir()
	g := newTestGateAt(t, root, "version: 1\n", false)

	script := &editScript{answers: []*mcpsdk.ElicitResult{accept(map[string]any{"remember": true})}}
	cs := connectGate(t, g, protocolLegacy, script.handle)

	out, refusal, err := callGated(t, cs, "cg_run", map[string]any{"command": []string{"echo", "hi", secretToken}})
	if err != nil || refusal != "" {
		t.Fatalf("cg_run: err=%v refusal=%q", err, refusal)
	}
	if code, ok := out["exit_code"].(float64); !ok || code != 0 {
		t.Errorf("exit_code = %v, want 0", out["exit_code"])
	}

	warning, _ := out["remember_warning"].(string)
	for _, want := range []string{"skipping remember", "credential at token 3", "Uncheck remember"} {
		if !strings.Contains(warning, want) {
			t.Errorf("remember_warning lacks %q: %q", want, warning)
		}
	}
	if strings.Contains(warning, secretToken) {
		t.Errorf("remember_warning echoes the token: %q", warning)
	}
	if len(script.shown) != 1 {
		t.Errorf("forms shown = %d, want 1", len(script.shown))
	}

	if file, _ := readProjectIfPresent(root); strings.Contains(file, "echo") {
		t.Errorf("a rule was written:\n%s", file)
	}
}

func TestSecondRoundDescriptionNamesTheCredentialOnce(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	g := newTestGateAt(t, t.TempDir(), "version: 1\n", false)

	call := multiRound(testCall([]string{"echo", "hi", secretToken}))
	pending := firstRound(t, g, call)

	second, err := g.check(retryCall(call, pending, mcpsdk.InputResponseMap{"approval-1": accept(map[string]any{"remember": true})}))
	if err != nil || second.Pending == nil {
		t.Fatalf("retry = %+v, %v; want a second round", second, err)
	}

	got := ruleDescriptionOf(t, second.Pending, "approval-1")
	if n := strings.Count(got, "looks like a credential"); n != 1 {
		t.Errorf("description names the credential %d times, want once:\n%s", n, got)
	}

	root := g.store.Project.Path
	want := "The rule holds what looks like a credential at token 3, so nothing was saved. " +
		"The project file may be committed, and a pushed secret cannot be recalled. " +
		"Uncheck remember, or write the rule by hand in " + root + ". " +
		"Or trim the rule to the tokens before token 3. A trim allows any command that starts with those tokens, so it widens the rule. " +
		"The field holds the edit that failed. " +
		"An exact rule fixes every argument, and its first token decides which programs it covers. " +
		"Leave the rule as it is for an exact rule, tick the box for a prefix rule of the whole command, " +
		"or trim it to a leading part for a prefix rule that allows any command starting with those tokens. " +
		"Edited only when remembering; a YAML list of argv tokens, e.g. [go, test] or [foo, \"bar baz\"]"
	if got != want {
		t.Errorf("description =\n%s\nwant\n%s", got, want)
	}
}
