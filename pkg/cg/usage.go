package cg

import (
	"fmt"
	"time"
)

// Usage source tags record which mechanism produced a run's numbers.
const (
	// UsageSourceRusageChildren is the reaped child's wait4 rusage. It carries
	// the full field set on Unix.
	UsageSourceRusageChildren = "rusage_children"

	// UsageSourceProcessState is the portable CPU-only fallback used on
	// platforms without a rusage struct. Only the CPU times are populated.
	UsageSourceProcessState = "process_state"

	// UsageSourceCgroupV2 is the Linux cgroup v2 subtree accounting added by a
	// later milestone.
	UsageSourceCgroupV2 = "cgroup_v2"
)

// Usage is the resource accounting for a finished run. CPU times are portable
// and always populated. The memory and counter fields come from the Unix rusage
// struct and stay zero on platforms without one. The complete set is always
// written so a machine consumer sees a stable shape.
type Usage struct {
	Source                 string `json:"source"`
	UserUS                 int64  `json:"user_us"`
	SystemUS               int64  `json:"system_us"`
	MaxRSSBytes            int64  `json:"maxrss_bytes"`
	MinorFaults            int64  `json:"minor_faults"`
	MajorFaults            int64  `json:"major_faults"`
	VoluntaryCtxSwitches   int64  `json:"voluntary_ctx_switches"`
	InvoluntaryCtxSwitches int64  `json:"involuntary_ctx_switches"`
	BlockInputOps          int64  `json:"block_input_ops"`
	BlockOutputOps         int64  `json:"block_output_ops"`
}

func (u Usage) userDuration() time.Duration {
	return time.Duration(u.UserUS) * time.Microsecond
}

func (u Usage) systemDuration() time.Duration {
	return time.Duration(u.SystemUS) * time.Microsecond
}

// cpuToken renders the compact cpu=user/sys token for the Finished line. It is
// the single most diagnostic usage signal, so it rides the default line.
func (u Usage) cpuToken() string {
	return fmt.Sprintf("cpu=%s/%s", formatDuration(u.userDuration()), formatDuration(u.systemDuration()))
}

// formatUsage renders the full verbose "Usage ..." line emitted under -v. It
// carries the memory peak, page faults, context switches, block I/O counts, and
// the source tag so the choice of mechanism is auditable.
func formatUsage(u Usage) string {
	return fmt.Sprintf(
		"Usage user=%s sys=%s maxrss=%s minflt=%d majflt=%d nvcsw=%d nivcsw=%d inblock=%d oublock=%d source=%s",
		formatDuration(u.userDuration()), formatDuration(u.systemDuration()), formatBytes(u.MaxRSSBytes),
		u.MinorFaults, u.MajorFaults, u.VoluntaryCtxSwitches, u.InvoluntaryCtxSwitches,
		u.BlockInputOps, u.BlockOutputOps, u.Source,
	)
}

// formatBytes renders a byte count with a binary-scaled unit suffix. Values
// under 1 KiB show as a plain byte count. Larger values show one decimal place.
func formatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}

	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}

	return fmt.Sprintf("%.1f%cB", float64(n)/float64(div), "KMGTPE"[exp])
}
