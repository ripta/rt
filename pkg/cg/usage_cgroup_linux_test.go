//go:build linux

package cg

import (
	"os"
	"os/exec"
	"testing"
)

// cgroupProbe reports whether a writable cgroup v2 group can be created in this
// environment. It creates and tears down a throwaway group so a skip decision
// costs nothing beyond one mkdir.
func cgroupProbe(t *testing.T) {
	t.Helper()

	c := prepareCgroup(exec.Command("true"))
	if c == nil {
		t.Skip("no writable cgroup v2 available")
	}
	c.close()
}

func TestCgroupCollectSubtree(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	cgroupProbe(t)

	// A shell that forks a child which itself forks a grandchild. The grandchild
	// burns cpu. RUSAGE_CHILDREN would account only the reaped tree; the cgroup
	// accounts the whole subtree and reports at least three live pids at peak.
	cmd := exec.Command("sh", "-c", "( sh -c 'i=0; while [ $i -lt 200000 ]; do i=$((i+1)); done' ) &\nwait")
	cmd.SysProcAttr = nil

	c := prepareCgroup(cmd)
	if c == nil {
		t.Fatal("prepareCgroup returned nil after probe succeeded")
	}
	if err := cmd.Run(); err != nil {
		t.Fatalf("running command: %v", err)
	}

	u, ok := c.collect()
	if !ok {
		t.Fatal("collect reported failure with a working cgroup")
	}
	c.close()

	if u.Source != UsageSourceCgroupV2 {
		t.Errorf("source = %q, want %q", u.Source, UsageSourceCgroupV2)
	}
	if u.UserUS+u.SystemUS == 0 {
		t.Errorf("expected nonzero cpu time for a busy loop, got user=%d system=%d", u.UserUS, u.SystemUS)
	}
	if u.PeakPids < 2 {
		t.Errorf("peak pids = %d, want >= 2 for a forking subtree", u.PeakPids)
	}
}

func TestCgroupCloseRemovesGroup(t *testing.T) {
	cgroupProbe(t)

	c := prepareCgroup(exec.Command("true"))
	if c == nil {
		t.Fatal("prepareCgroup returned nil after probe succeeded")
	}

	lc, ok := c.(*linuxCgroup)
	if !ok {
		t.Fatalf("collector type = %T, want *linuxCgroup", c)
	}
	dir := lc.dir

	c.close()

	if _, err := os.Stat(dir); err == nil {
		t.Errorf("cgroup dir %s still present after close", dir)
	}
}
