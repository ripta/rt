package model

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func runStatsCommand(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()

	var out, errOut bytes.Buffer

	root := newTestRoot()
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs(append([]string{"stats"}, args...))

	err = root.Execute()

	return out.String(), errOut.String(), err
}

type statsGoldenTest struct {
	Name   string
	Args   []string
	Golden string
}

var statsCommand = []string{"--", "make", "test", "JOBS=10", "FOO=file"}

var statsGoldenTests = []statsGoldenTest{
	{
		Name:   "default table",
		Args:   append([]string{"--cwd", "/"}, statsCommand...),
		Golden: "stats/default.stdout.golden",
	},
	{
		Name:   "default json",
		Args:   append([]string{"--cwd", "/", "-o", "json"}, statsCommand...),
		Golden: "stats/default-json.stdout.golden",
	},
	{
		Name:   "standalone only table",
		Args:   append([]string{"--cwd", "/", "--pool", "none"}, statsCommand...),
		Golden: "stats/pool-none.stdout.golden",
	},
	{
		Name:   "pooled only json",
		Args:   append([]string{"--cwd", "/", "--pool", "only", "-o", "json"}, statsCommand...),
		Golden: "stats/pool-only-json.stdout.golden",
	},
	{
		Name:   "no samples json",
		Args:   []string{"--cwd", "/", "-o", "json", "--", "cargo", "build"},
		Golden: "stats/no-samples-json.stdout.golden",
	},
	{
		Name:   "recent samples and failures",
		Args:   append([]string{"--cwd", "/", "--samples", "1", "--exit-code", "!=0", "-o", "json"}, statsCommand...),
		Golden: "stats/samples-exit-json.stdout.golden",
	},
}

func TestStatsCommandGolden(t *testing.T) {
	h := newFixtureHistory(t)
	appendFixtureRuns(t, h, statsFixtureRuns)

	for _, test := range statsGoldenTests {
		t.Run(test.Name, func(t *testing.T) {
			stdout, stderr, err := runStatsCommand(t, test.Args...)
			if err != nil {
				t.Fatalf("stats: %v\n%s", err, stderr)
			}

			assertGolden(t, test.Golden, stdout)
		})
	}
}

func TestStatsCommandJSONMatchesQuery(t *testing.T) {
	h := newFixtureHistory(t)
	appendFixtureRuns(t, h, statsFixtureRuns)

	stdout, _, err := runStatsCommand(t, append([]string{"--cwd", "/", "-o", "json"}, statsCommand...)...)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}

	want, err := json.MarshalIndent(queryStats(t, h, fixtureQuery("make test JOBS=10 FOO=file")), "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.TrimSpace(stdout) != string(want) {
		t.Errorf("CLI JSON differs from the query result\n got: %s\nwant: %s", stdout, want)
	}
}

type statsUsageTest struct {
	Name string
	Args []string
	Want string
}

var statsUsageTests = []statsUsageTest{
	{"no command", []string{"--cwd", "/"}, "requires a command"},
	{"bad pool", []string{"--pool", "some", "--", "make"}, "pool"},
	{"zero samples", []string{"--samples", "0", "--", "make"}, "samples"},
	{"too many samples", []string{"--samples", "1001", "--", "make"}, "samples"},
	{"bad exit code", []string{"--exit-code", "abc", "--", "make"}, "--exit-code"},
	{"bad since", []string{"--since", "whenever", "--", "make"}, "--since"},
	{"bad output", []string{"-o", "xml", "--", "make"}, "--output"},
}

func TestStatsCommandUsageErrors(t *testing.T) {
	newFixtureHistory(t)

	for _, test := range statsUsageTests {
		t.Run(test.Name, func(t *testing.T) {
			stdout, stderr, err := runStatsCommand(t, test.Args...)

			var exitErr *ExitError
			if !errors.As(err, &exitErr) || exitErr.Code != 2 {
				t.Fatalf("err = %v, want exit code 2", err)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want empty", stdout)
			}
			if !strings.Contains(stderr, test.Want) {
				t.Errorf("stderr = %q, want it to mention %q", stderr, test.Want)
			}
		})
	}
}
