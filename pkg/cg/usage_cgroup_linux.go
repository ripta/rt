//go:build linux

package cg

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// cgroupMount is the conventional cgroup v2 unified-hierarchy mount point. The
// presence of cgroup.controllers under it confirms cgroup v2 is active.
const cgroupMount = "/sys/fs/cgroup"

// linuxCgroup is a fresh cgroup v2 group created for a single child run. The
// child joins it at clone time via CLONE_INTO_CGROUP, so the whole subtree is
// accounted regardless of reaping. dir is the group directory under the unified
// hierarchy; fd is that directory opened for the SysProcAttr handoff.
type linuxCgroup struct {
	dir string
	fd  int
}

// prepareCgroup creates a dedicated cgroup v2 group and wires child to clone
// into it. It returns nil on any failure so the caller falls back to the wait4
// baseline. The parent's SysProcAttr is preserved; only the cgroup fields are
// added.
func prepareCgroup(child *exec.Cmd) cgroupCollector {
	if _, err := os.Stat(filepath.Join(cgroupMount, "cgroup.controllers")); err != nil {
		return nil
	}

	parent, err := selfCgroupDir()
	if err != nil {
		return nil
	}

	dir, err := os.MkdirTemp(parent, "cg-*")
	if err != nil {
		return nil
	}

	// Best-effort delegation of the controllers we read. Where the parent forbids
	// it, cpu.stat is still present and the run reports what it can.
	_ = os.WriteFile(filepath.Join(parent, "cgroup.subtree_control"), []byte("+cpu +memory +pids +io"), 0)

	fd, err := syscall.Open(dir, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC, 0)
	if err != nil {
		_ = os.Remove(dir)
		return nil
	}

	if child.SysProcAttr == nil {
		child.SysProcAttr = &syscall.SysProcAttr{}
	}
	child.SysProcAttr.UseCgroupFD = true
	child.SysProcAttr.CgroupFD = fd

	return &linuxCgroup{dir: dir, fd: fd}
}

// selfCgroupDir resolves the calling process's own cgroup directory under the
// unified hierarchy from /proc/self/cgroup. The v2 entry is the line prefixed
// with "0::".
func selfCgroupDir() (string, error) {
	f, err := os.Open("/proc/self/cgroup")
	if err != nil {
		return "", err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		rel, ok := strings.CutPrefix(sc.Text(), "0::")
		if !ok {
			continue
		}
		return filepath.Join(cgroupMount, filepath.Clean("/"+rel)), nil
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	return "", os.ErrNotExist
}

// collect reads the subtree accounting after the child has exited. cpu.stat is
// the minimum: its absence means the group never worked, so collect reports a
// failure and the caller falls back. memory.peak, pids.peak, and io.stat are
// best-effort and stay zero where the controller was not delegated.
func (c *linuxCgroup) collect() (Usage, bool) {
	cpu := c.readKV("cpu.stat")
	user, ok := cpu["user_usec"]
	sys := cpu["system_usec"]
	if !ok {
		return Usage{}, false
	}

	u := Usage{
		Source:      UsageSourceCgroupV2,
		UserUS:      user,
		SystemUS:    sys,
		MaxRSSBytes: c.readInt("memory.peak"),
		PeakPids:    c.readInt("pids.peak"),
	}
	u.BlockInputOps, u.BlockOutputOps = c.readIOOps()
	return u, true
}

// close reaps any straggling descendants left in the group, releases the fd,
// and removes the group. Every step is best-effort. A leftover group is a minor
// leak, not a run failure.
func (c *linuxCgroup) close() {
	_ = os.WriteFile(filepath.Join(c.dir, "cgroup.kill"), []byte("1"), 0)
	_ = syscall.Close(c.fd)
	_ = os.Remove(c.dir)
}

// readKV parses a flat "key value" cgroup stat file into a map. Missing files
// and unparsable lines yield an empty or partial map; the caller decides what a
// missing key means.
func (c *linuxCgroup) readKV(name string) map[string]int64 {
	out := make(map[string]int64)
	f, err := os.Open(filepath.Join(c.dir, name))
	if err != nil {
		return out
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), " ")
		if !ok {
			continue
		}
		if n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
			out[k] = n
		}
	}
	return out
}

// readInt reads a single-value cgroup file such as memory.peak or pids.peak.
// A missing or unparsable file yields zero.
func (c *linuxCgroup) readInt(name string) int64 {
	data, err := os.ReadFile(filepath.Join(c.dir, name))
	if err != nil {
		return 0
	}
	n, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// readIOOps sums the per-device read and write operation counts from io.stat.
// Each line is "MAJ:MIN key=value ...". rios and wios are the read and write
// I/O operation counts issued to the device.
func (c *linuxCgroup) readIOOps() (int64, int64) {
	f, err := os.Open(filepath.Join(c.dir, "io.stat"))
	if err != nil {
		return 0, 0
	}
	defer f.Close()

	var rios, wios int64
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		for _, field := range strings.Fields(sc.Text()) {
			k, v, ok := strings.Cut(field, "=")
			if !ok {
				continue
			}
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				continue
			}
			switch k {
			case "rios":
				rios += n
			case "wios":
				wios += n
			}
		}
	}
	return rios, wios
}
