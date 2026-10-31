package approve

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

var (
	ghToken  = "ghp_" + strings.Repeat("aB3", 12)
	skToken  = "sk-proj-" + strings.Repeat("Ab1", 12)
	awsKey   = "AKIA" + "IOSFODNN7EXAMPL" + "E"
	slackTok = "xoxb-1234567890-abcdefghij"
)

type credentialTest struct {
	Name   string
	Tokens []string
	Want   []int
}

var credentialTests = []credentialTest{
	{Name: "authorization header", Tokens: []string{"curl", "-H", "Authorization: Basic dXNlcjpwYXNz", "https://x"}, Want: []int{2}},
	{Name: "header name in another case", Tokens: []string{"curl", "-H", "AUTHORIZATION:abc"}, Want: []int{2}},
	{Name: "proxy authorization", Tokens: []string{"curl", "-H", "Proxy-Authorization: Basic abc"}, Want: []int{2}},
	{Name: "cookie header", Tokens: []string{"curl", "--header=Cookie: sid=abc"}, Want: []int{1}},
	{Name: "header split across tokens", Tokens: []string{"http", "Authorization:", "opaque"}, Want: []int{2}},
	{Name: "authorization glued to a short flag", Tokens: []string{"curl", "-HAuthorization: Basic dXNlcjpwYXNz"}, Want: []int{1}},
	{Name: "proxy authorization glued to a short flag", Tokens: []string{"curl", "-HProxy-Authorization: Basic abc"}, Want: []int{1}},
	{Name: "cookie glued to a short flag", Tokens: []string{"curl", "-HCookie: sid=abc"}, Want: []int{1}},
	{Name: "authorization name after a long flag with equals", Tokens: []string{"curl", "--header=Authorization:", "Basic dXNlcjpwYXNz"}, Want: []int{2}},
	{Name: "proxy authorization name after a long flag with equals", Tokens: []string{"curl", "--header=Proxy-Authorization:", "Basic abc"}, Want: []int{2}},
	{Name: "cookie name after a long flag with equals", Tokens: []string{"curl", "--header=Cookie:", "sid=abc"}, Want: []int{2}},
	{Name: "authorization name glued to a short flag", Tokens: []string{"curl", "-HAuthorization:", "Basic abc"}, Want: []int{2}},
	{Name: "long flag with equals and an empty header", Tokens: []string{"curl", "--header=Authorization:"}},
	{Name: "empty header value", Tokens: []string{"curl", "-H", "Authorization:"}},
	{Name: "header name in prose", Tokens: []string{"echo", "the Authorization header is set"}},
	{Name: "header name mid token", Tokens: []string{"grep", "x-Cookie: foo"}},
	{Name: "bearer value", Tokens: []string{"curl", "-H", "X-Auth: Bearer abcdef0123456789abcdef"}, Want: []int{2}},
	{Name: "bearer split", Tokens: []string{"tool", "Bearer", "abcdef0123456789abcdef"}, Want: []int{2}},
	{Name: "bearer flag", Tokens: []string{"curl", "--oauth2-bearer", "whatever"}, Want: []int{2}},
	{Name: "bearer prose", Tokens: []string{"echo", "Bearer authentication is used"}},
	{Name: "bearer short", Tokens: []string{"echo", "bearer abc123"}},
	{Name: "url userinfo", Tokens: []string{"git", "clone", "https://user:hunter2@example.com/r.git"}, Want: []int{2}},
	{Name: "database url", Tokens: []string{"psql", "postgres://app:pw@db:5432/x"}, Want: []int{1}},
	{Name: "url with an empty user", Tokens: []string{"redis-cli", "-u", "redis://:s3cret@host:6379"}, Want: []int{2}},
	{Name: "url with an empty user and a path", Tokens: []string{"tool", "amqp://:pw@host/vhost"}, Want: []int{1}},
	{Name: "url with an empty user and password", Tokens: []string{"tool", "redis://:@host"}},
	{Name: "url without userinfo", Tokens: []string{"curl", "https://example.com:8080/a@b"}},
	{Name: "ssh url with a user only", Tokens: []string{"git", "clone", "ssh://git@github.com/o/r.git"}},
	{Name: "scp-style remote", Tokens: []string{"git", "clone", "git@github.com:o/r.git"}},
	{Name: "github token", Tokens: []string{"gh", "auth", ghToken}, Want: []int{2}},
	{Name: "github token after an equals sign", Tokens: []string{"tool", "--token=" + ghToken}, Want: []int{1}},
	{Name: "github token too short", Tokens: []string{"tool", "ghp_abc123"}},
	{Name: "fine-grained github token", Tokens: []string{"tool", "github_pat_" + strings.Repeat("A1_", 20)}, Want: []int{1}},
	{Name: "openai style key", Tokens: []string{"tool", skToken}, Want: []int{1}},
	{Name: "branch named sk-foo", Tokens: []string{"git", "checkout", "sk-foo"}},
	{Name: "long branch starting with sk-", Tokens: []string{"git", "checkout", "sk-" + strings.Repeat("feature-", 5)}},
	{Name: "word ending in sk-", Tokens: []string{"tool", "task-" + strings.Repeat("A1", 20)}},
	{Name: "aws access key", Tokens: []string{"aws", "--key", awsKey}, Want: []int{2}},
	{Name: "AKIA prefix too short", Tokens: []string{"tool", "AKIASHORT"}},
	{Name: "slack token", Tokens: []string{"tool", slackTok}, Want: []int{1}},
	{Name: "plain words", Tokens: []string{"go", "test", "./...", "-run", "TestFoo"}},
	{Name: "two credentials", Tokens: []string{"tool", ghToken, "x", awsKey}, Want: []int{1, 3}},
}

func TestCredentialPositions(t *testing.T) {
	t.Parallel()

	for _, test := range credentialTests {
		t.Run(test.Name, func(t *testing.T) {
			t.Parallel()

			got := CredentialPositions(test.Tokens)
			if !slices.Equal(got, test.Want) {
				t.Errorf("CredentialPositions(%q) = %v, want %v", test.Tokens, got, test.Want)
			}
		})
	}
}

func TestRedactCredentialsKeepsTheRest(t *testing.T) {
	t.Parallel()

	in := []string{"tool", ghToken, "x"}
	got := RedactCredentials(in)

	if want := []string{"tool", CredentialPlaceholder, "x"}; !slices.Equal(got, want) {
		t.Errorf("RedactCredentials = %q, want %q", got, want)
	}
	if in[1] != ghToken {
		t.Errorf("the input was modified")
	}
}

func TestCredentialText(t *testing.T) {
	t.Parallel()

	if !CredentialText("[curl, " + ghToken + "") {
		t.Errorf("unterminated list with a credential should match")
	}
	if !CredentialText("Authorization: Bearer abcdef0123456789abcdef") {
		t.Errorf("bare header text should match")
	}
	if CredentialText("not a list") {
		t.Errorf("plain text should not match")
	}
}

type appendCredentialTest struct {
	Name   string
	Exact  bool
	Tokens []string
}

var appendCredentialTests = []appendCredentialTest{
	{Name: "exact with a token", Exact: true, Tokens: []string{"gh", "auth", ghToken}},
	{Name: "prefix with a header", Tokens: []string{"curl", "-H", "Authorization: Basic abc"}},
}

func TestAppendRefusesACredential(t *testing.T) {
	t.Parallel()

	for _, test := range appendCredentialTests {
		t.Run(test.Name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			s := loadProject(t, root)

			var err error
			if test.Exact {
				err = s.AppendProjectAllowExact(test.Tokens, WriteDirect)
			} else {
				err = s.AppendProjectAllowPrefix(test.Tokens, WriteDirect)
			}

			if !errors.Is(err, ErrCredentialToken) {
				t.Fatalf("err = %v, want ErrCredentialToken", err)
			}
			for _, tok := range test.Tokens[1:] {
				if strings.Contains(err.Error(), tok) {
					t.Errorf("error echoes a token: %v", err)
				}
			}
			if !strings.Contains(err.Error(), "token") {
				t.Errorf("error should name a position: %v", err)
			}

			if _, statErr := os.Stat(ProjectPath(root, "")); !os.IsNotExist(statErr) {
				t.Errorf("a file was written: %v", statErr)
			}
		})
	}
}

func TestAppendChecksOnlyTheRuleBeingAppended(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	existing := "version: 1\nallow:\n  - prefix: [gh, auth, " + ghToken + "]\n"
	if err := os.MkdirAll(filepath.Dir(ProjectPath(root, "")), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ProjectPath(root, ""), []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}

	s := loadProject(t, root)
	if err := s.AppendProjectAllowPrefix([]string{"make"}, WriteDirect); err != nil {
		t.Fatalf("append beside an existing credential-shaped rule: %v", err)
	}

	raw, err := os.ReadFile(ProjectPath(root, ""))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "prefix: [make]") || !strings.Contains(string(raw), ghToken) {
		t.Errorf("file should hold both rules:\n%s", raw)
	}
}
