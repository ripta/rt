package model

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"time"
)

// SandboxExecPath is the Seatbelt launcher. A sandboxed run execs it with the frozen profile text,
// and it execs the command in place, so the command keeps the run's pid.
const SandboxExecPath = "/usr/bin/sandbox-exec"

const (
	// profileCompileStatus is the exit status sandbox-exec uses for a profile that does not compile.
	profileCompileStatus = 65

	// execFailureStatus is the status of a profile that applied and then denied the exec itself.
	execFailureStatus = 71

	nestedStderr      = "sandbox-exec: sandbox_apply: Operation not permitted"
	execFailurePrefix = "sandbox-exec: execvp()"

	probeTimeout = 30 * time.Second
)

// SandboxClass says whether a profile can be applied from this process.
type SandboxClass string

const (
	// SandboxApplies means the probe ran, or failed only to exec the probe command. The profile is
	// healthy and this process is not inside another sandbox.
	SandboxApplies SandboxClass = "applies"

	// SandboxNested means the probe exited with exactly the line sandbox-exec prints when the process
	// already runs inside a sandbox.
	SandboxNested SandboxClass = "nested"

	// SandboxUnknown is every other outcome, a compile failure included.
	SandboxUnknown SandboxClass = "unknown"
)

// SandboxProbe is the outcome of applying a profile to /usr/bin/true. Stderr is the launcher's own
// message, trimmed. CompileFailed is set on exit status 65 only.
type SandboxProbe struct {
	Class         SandboxClass
	CompileFailed bool
	ExitCode      int
	Stderr        string
}

// ProbeSeatbelt applies the profile text to /usr/bin/true with the given -D parameters, each spelled
// KEY=VALUE, and classifies the result. The probe runs the real profile and not a stand-in, since an
// outer sandbox can block one and not the other.
//
// The compile check runs before Seatbelt applies the profile, so it gives the same answer inside and
// outside an outer sandbox. Status 71 is not a compile failure. It also appears when the profile
// denies running /usr/bin/true, and when this process is already sandboxed.
func ProbeSeatbelt(profile string, defines []string) SandboxProbe {
	args := []string{"-p", profile}
	for _, define := range defines {
		args = append(args, "-D", define)
	}
	args = append(args, "/usr/bin/true")

	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, SandboxExecPath, args...)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err := cmd.Run()

	var exit *exec.ExitError
	switch {
	case err == nil:
		return classifyProbe(0, stderr.String(), nil)
	case errors.As(err, &exit):
		return classifyProbe(exit.ExitCode(), stderr.String(), nil)
	default:
		return classifyProbe(-1, stderr.String(), err)
	}
}

// classifyProbe turns the probe's exit status and stderr into a class. Only an exact match counts as
// nested, so a change in sandbox-exec's wording makes the answer unknown, which refuses.
func classifyProbe(exit int, stderr string, spawnErr error) SandboxProbe {
	out := SandboxProbe{Class: SandboxUnknown, ExitCode: exit, Stderr: strings.TrimSpace(stderr)}

	switch {
	case spawnErr != nil:
		if out.Stderr == "" {
			out.Stderr = spawnErr.Error()
		}
	case exit == 0:
		out.Class = SandboxApplies
	case exit == profileCompileStatus:
		out.CompileFailed = true
	case exit == execFailureStatus && out.Stderr == nestedStderr:
		out.Class = SandboxNested
	case exit == execFailureStatus && strings.HasPrefix(out.Stderr, execFailurePrefix) && !strings.Contains(out.Stderr, "\n"):
		out.Class = SandboxApplies
	}

	return out
}

// SandboxSpec is what a sandboxed run carries beyond the fields every supervisor already honors. A
// supervisor that knows it records Command and Name, checks the command's path, and runs the
// preflight. A supervisor that does not ignores it and still execs the encoded argv sandboxed.
//
// Command is the typed command. Resolved and Canonical are the command's own paths, as the approval
// gate saw them. Profile is the frozen profile text and Defines its -D parameters, each KEY=VALUE.
type SandboxSpec struct {
	Name      string   `json:"name"`
	Command   []string `json:"command"`
	Resolved  string   `json:"resolved"`
	Canonical string   `json:"canonical,omitempty"`
	Profile   string   `json:"profile"`
	Defines   []string `json:"defines,omitempty"`

	// Class is the load probe's classification of the profile, which the preflight compares its own
	// result against. An empty class is a spec from a server that only ever ran profiles that applied.
	Class SandboxClass `json:"class,omitempty"`

	// Skipped is set when the command runs without the profile, because cg is inside an outer sandbox
	// and the user's rule or approval allows that. It holds the reason for the run record. The
	// command is then launched as typed, with no sandbox-exec argv, and nothing is preflighted.
	Skipped string `json:"skipped,omitempty"`
}

// SandboxLaunch encodes a sandboxed run into the spec fields every supervisor honors. The argv runs
// sandbox-exec with the profile text, the parameters, the command's resolved path, and its arguments
// with each inner pin inlined. The resolution names sandbox-exec as both paths. Nothing here relies
// on the inner-exec pin, which a supervisor older than it would drop.
func SandboxLaunch(sb *SandboxSpec, inner map[int]string) ([]string, *Resolution) {
	argv := []string{SandboxExecPath, "-p", sb.Profile}
	for _, define := range sb.Defines {
		argv = append(argv, "-D", define)
	}

	args := slices.Clone(sb.Command[1:])
	for offset, path := range inner {
		if offset >= 1 && offset < len(sb.Command) {
			args[offset-1] = path
		}
	}

	argv = append(argv, sb.Resolved)
	argv = append(argv, args...)

	return argv, &Resolution{Argv: argv, Resolved: SandboxExecPath, Canonical: SandboxExecPath}
}

// Preflight runs just before each spawn. It refuses a command whose resolved path no longer
// canonicalizes to the path the gate saw, then compiles the frozen profile once more. A profile
// that imports another file can stop compiling after load, and this turns that into a refusal that
// names the compile error, not the command's own exit status 65.
//
// The probe is classified the way the load probe was. A class that differs from the load-time one
// refuses with the probe's stderr, and so does any class other than applies. cg never falls back to
// running without the profile. A skipped profile has nothing to preflight.
func (s *SandboxSpec) Preflight() error {
	return s.preflight(ProbeSeatbelt)
}

func (s *SandboxSpec) preflight(probeSeatbelt func(profile string, defines []string) SandboxProbe) error {
	if s.Skipped != "" {
		return nil
	}

	if s.Resolved == "" {
		return fmt.Errorf("sandbox %q: the command has no resolved path to run", s.Name)
	}

	if err := (&Resolution{Resolved: s.Resolved, Canonical: s.Canonical}).VerifyCanonical(); err != nil {
		return fmt.Errorf("sandbox %q: %w", s.Name, err)
	}

	want := s.Class
	if want == "" {
		want = SandboxApplies
	}

	probe := probeSeatbelt(s.Profile, s.Defines)
	switch {
	case probe.CompileFailed:
		return fmt.Errorf("sandbox %q: its profile no longer compiles: %s", s.Name, probe.Stderr)
	case probe.Class != want:
		return fmt.Errorf("sandbox %q: its profile was classified as %s at load and %s now: %s",
			s.Name, want, probe.Class, probe.Stderr)
	case probe.Class != SandboxApplies:
		return fmt.Errorf("sandbox %q: the profile cannot be applied (%s): %s", s.Name, probe.Class, probe.Stderr)
	}

	return nil
}

// ProxyEnv returns the variables that point a command at a filtering proxy. The no-proxy list keeps
// loopback traffic direct.
func ProxyEnv(proxy string) map[string]string {
	const loopback = "localhost,127.0.0.1,::1"

	return map[string]string{
		"HTTP_PROXY":  proxy,
		"HTTPS_PROXY": proxy,
		"http_proxy":  proxy,
		"https_proxy": proxy,
		"NO_PROXY":    loopback,
		"no_proxy":    loopback,
	}
}

// LayerEnv merges environment overrides, later layers winning. It returns nil when no layer has an
// entry, and never changes its arguments.
func LayerEnv(layers ...map[string]string) map[string]string {
	var out map[string]string

	for _, layer := range layers {
		for k, v := range layer {
			if out == nil {
				out = make(map[string]string)
			}
			out[k] = v
		}
	}

	return out
}
