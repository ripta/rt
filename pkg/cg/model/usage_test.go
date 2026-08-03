package model

import (
	"os/exec"
	"testing"
)

func TestCollectUsageRealCommand(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	cmd := exec.Command("sh", "-c", "i=0; while [ $i -lt 200000 ]; do i=$((i+1)); done")
	if err := cmd.Run(); err != nil {
		t.Fatalf("running command: %v", err)
	}

	u := collectUsage(cmd)
	if u.Source != UsageSourceRusageChildren {
		t.Errorf("source = %q, want %q", u.Source, UsageSourceRusageChildren)
	}
	if u.UserUS < 0 || u.SystemUS < 0 {
		t.Errorf("negative cpu times: user=%d system=%d", u.UserUS, u.SystemUS)
	}
	if u.UserUS+u.SystemUS == 0 {
		t.Errorf("expected nonzero cpu time for a busy loop, got user=%d system=%d", u.UserUS, u.SystemUS)
	}
	if u.MaxRSSBytes <= 0 {
		t.Errorf("maxrss = %d bytes, want > 0", u.MaxRSSBytes)
	}
}

func TestCollectUsageNilProcessState(t *testing.T) {
	// A command that was never started has no ProcessState. Collection must not
	// panic and still reports the source tag.
	u := collectUsage(exec.Command("true"))
	if u.Source != UsageSourceRusageChildren {
		t.Errorf("source = %q, want %q", u.Source, UsageSourceRusageChildren)
	}
}

type cpuTokenTest struct {
	name string
	u    Usage
	want string
}

var cpuTokenTests = []cpuTokenTest{
	{name: "zero", u: Usage{}, want: "cpu=0s/0s"},
	{name: "split", u: Usage{UserUS: 8000, SystemUS: 3000}, want: "cpu=8ms/3ms"},
}

func TestUsageCPUToken(t *testing.T) {
	t.Parallel()

	for _, tt := range cpuTokenTests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.u.cpuToken(); got != tt.want {
				t.Errorf("cpuToken() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatUsage(t *testing.T) {
	t.Parallel()

	u := Usage{
		Source:                 UsageSourceRusageChildren,
		UserUS:                 8000,
		SystemUS:               3000,
		MaxRSSBytes:            4404019,
		PeakPids:               7,
		MinorFaults:            120,
		MajorFaults:            0,
		VoluntaryCtxSwitches:   5,
		InvoluntaryCtxSwitches: 2,
		BlockInputOps:          0,
		BlockOutputOps:         8,
	}
	want := "Usage user=8ms sys=3ms maxrss=4.2MB pids=7 minflt=120 majflt=0 nvcsw=5 nivcsw=2 inblock=0 oublock=8 source=rusage_children"
	if got := formatUsage(u); got != want {
		t.Errorf("formatUsage() = %q, want %q", got, want)
	}
}

type formatBytesTest struct {
	name string
	in   int64
	want string
}

var formatBytesTests = []formatBytesTest{
	{name: "zero", in: 0, want: "0B"},
	{name: "sub-kib", in: 512, want: "512B"},
	{name: "one kib", in: 1024, want: "1.0KB"},
	{name: "one and a half kib", in: 1536, want: "1.5KB"},
	{name: "mib", in: 4404019, want: "4.2MB"},
	{name: "gib", in: 3 * 1024 * 1024 * 1024, want: "3.0GB"},
}

func TestFormatBytes(t *testing.T) {
	t.Parallel()

	for _, tt := range formatBytesTests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatBytes(tt.in); got != tt.want {
				t.Errorf("formatBytes(%d) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
