package cg

import (
	"context"
	"errors"
	"time"
)

// waitPollInterval is the filesystem poll cadence used to detect a run's
// transition to finished. A separate CLI process has no in-process done signal,
// so it polls the run directory for the appearance of meta.json (finished) or
// debug.json (failed to start).
const waitPollInterval = 100 * time.Millisecond

// awaitFinish polls run id until it finishes, timeout elapses, or ctx is
// cancelled. A finished run (meta.json present) and a failed-to-start run
// (debug.json present) both count as finished. A false return with a nil error
// means the timeout fired. The caller must have already confirmed the run is in
// flight; awaitFinish only watches for the transition.
func awaitFinish(ctx context.Context, id string, timeout time.Duration) (bool, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	ticker := time.NewTicker(waitPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-timer.C:
			return false, nil
		case <-ctx.Done():
			return false, ctx.Err()
		case <-ticker.C:
			_, err := LookupRunDir(id)
			switch {
			case err == nil, errors.Is(err, ErrFailedRun):
				return true, nil
			case errors.Is(err, ErrIncompleteRun):
				continue
			default:
				return false, err
			}
		}
	}
}
