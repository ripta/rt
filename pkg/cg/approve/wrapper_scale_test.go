package approve

import (
	"runtime"
	"slices"
	"testing"
)

// longChainAllocBound is far above what a near-linear evaluation of a 10k-token
// chain allocates, and far below the gigabyte a per-layer copy of the argv cost.
const longChainAllocBound = 64 << 20

func TestEvaluateLongWrapperChainStaysNearLinear(t *testing.T) {
	// Not parallel: the measurement reads process-wide allocation counters.
	const tokens = 10000

	argv := slices.Repeat([]string{"nice"}, tokens)
	argv = append(argv, "make")

	tt := wrapperTest{allow: []Rule{wrapperRule("nice"), prefixRule("make")}}
	rs := buildWrapperRuleset(tt, false)

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)

	got := rs.Evaluate(Subject{Argv: argv, Canonical: argv}, nil, nil)

	runtime.ReadMemStats(&after)

	if got.Decision != DecisionRun || len(got.Chain) != tokens+1 {
		t.Fatalf("verdict = %v with chain %d, want run with %d layers", got.Decision, len(got.Chain), tokens+1)
	}
	if alloc := after.TotalAlloc - before.TotalAlloc; alloc > longChainAllocBound {
		t.Errorf("evaluating a %d-token chain allocated %d bytes, want under %d", tokens, alloc, longChainAllocBound)
	}
}
