package approve

import (
	"errors"
	"testing"
)

type parseTest struct {
	name    string
	yaml    string
	wantErr error // sentinel to match with errors.Is; nil means no error
}

var parseTests = []parseTest{
	{
		name: "minimal valid",
		yaml: "version: 1\n",
	},
	{
		name: "mode defaults to empty (enforce at merge)",
		yaml: "version: 1\n",
	},
	{
		name: "explicit allow-all",
		yaml: "version: 1\nmode: allow-all\n",
	},
	{
		name: "explicit deny-all",
		yaml: "version: 1\nmode: deny-all\n",
	},
	{
		name:    "unknown mode",
		yaml:    "version: 1\nmode: loose\n",
		wantErr: ErrUnknownMode,
	},
	{
		name:    "missing version",
		yaml:    "mode: enforce\n",
		wantErr: ErrUnknownVersion,
	},
	{
		name:    "wrong version",
		yaml:    "version: 2\n",
		wantErr: ErrUnknownVersion,
	},
	{
		name: "all four rule kinds across entries",
		yaml: `version: 1
deny:
  - regex: '(^|\s)sudo(\s|$)'
allow:
  - exact: [git, status]
  - prefix: [go, test]
  - glob: 'kubectl get *'
`,
	},
	{
		name: "deny with message ok",
		yaml: `version: 1
deny:
  - prefix: [rm, -rf]
    message: delete specific paths instead
`,
	},
	{
		name: "allow with permit_unsafe_envs ok",
		yaml: `version: 1
allow:
  - prefix: [make]
    permit_unsafe_envs: [PATH]
`,
	},
	{
		name: "no rule kind",
		yaml: `version: 1
deny:
  - message: nothing to match
`,
		wantErr: ErrNoRuleKind,
	},
	{
		name: "multiple rule kinds",
		yaml: `version: 1
allow:
  - exact: [git, status]
    prefix: [git]
`,
		wantErr: ErrMultipleRuleKinds,
	},
	{
		name: "message on allow rejected",
		yaml: `version: 1
allow:
  - prefix: [make]
    message: not allowed here
`,
		wantErr: ErrMessageOnAllow,
	},
	{
		name: "permit_unsafe_envs on deny rejected",
		yaml: `version: 1
deny:
  - prefix: [make]
    permit_unsafe_envs: [PATH]
`,
		wantErr: ErrPermitOnDeny,
	},
	{
		name: "empty glob is a present key",
		yaml: `version: 1
allow:
  - glob: ''
`,
	},
	{
		name: "invalid regex",
		yaml: `version: 1
deny:
  - regex: '('
`,
		wantErr: errInvalidPattern,
	},
	{
		name:    "malformed yaml",
		yaml:    "version: 1\ndeny: [unterminated\n",
		wantErr: errYAMLDecode,
	},
}

// errInvalidPattern and errYAMLDecode are test-only markers: the loader returns
// wrapped non-sentinel errors for these, so the cases assert that an error is
// returned rather than matching a specific sentinel.
var (
	errInvalidPattern = errors.New("invalid pattern")
	errYAMLDecode     = errors.New("yaml decode")
)

func TestParseDocument(t *testing.T) {
	t.Parallel()

	for _, tt := range parseTests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := ParseDocument([]byte(tt.yaml))

			switch tt.wantErr {
			case nil:
				if err != nil {
					t.Fatalf("ParseDocument() unexpected error: %v", err)
				}
			case errInvalidPattern, errYAMLDecode:
				if err == nil {
					t.Fatalf("ParseDocument() expected an error, got nil")
				}
			default:
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("ParseDocument() error = %v, want %v", err, tt.wantErr)
				}
			}
		})
	}
}

func TestParseDocumentRecordsKind(t *testing.T) {
	t.Parallel()

	src := `version: 1
deny:
  - regex: '(^|\s)sudo(\s|$)'
allow:
  - exact: [git, status]
  - prefix: [go, test]
  - glob: 'kubectl get *'
`
	_, doc, err := ParseDocument([]byte(src))
	if err != nil {
		t.Fatalf("ParseDocument() error: %v", err)
	}

	if got := doc.Deny[0].Kind(); got != KindRegex {
		t.Errorf("deny[0] kind = %v, want KindRegex", got)
	}
	wantAllow := []RuleKind{KindExact, KindPrefix, KindGlob}
	for i, want := range wantAllow {
		if got := doc.Allow[i].Kind(); got != want {
			t.Errorf("allow[%d] kind = %v, want %v", i, got, want)
		}
	}
}

type globTest struct {
	name string
	glob string
	want string
}

var globTests = []globTest{
	{name: "trailing star", glob: "kubectl get *", want: `^kubectl get .*$`},
	{name: "no wildcards", glob: "make", want: `^make$`},
	{name: "question mark", glob: "a?c", want: `^a.c$`},
	{name: "escapes metacharacters", glob: "a.b*c?", want: `^a\.b.*c.$`},
	{name: "escapes parens and plus", glob: "f(x)+", want: `^f\(x\)\+$`},
	{name: "empty", glob: "", want: `^$`},
}

func TestGlobToRegex(t *testing.T) {
	t.Parallel()

	for _, tt := range globTests {
		t.Run(tt.name, func(t *testing.T) {
			if got := globToRegex(tt.glob); got != tt.want {
				t.Errorf("globToRegex(%q) = %q, want %q", tt.glob, got, tt.want)
			}
		})
	}
}
