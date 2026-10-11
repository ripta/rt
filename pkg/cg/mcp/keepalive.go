package mcp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ripta/rt/pkg/cg/model"
)

// keepAliveInterval is how often a blocking wait sends a progress notification. Clients abort a
// call that stays silent for too long, so the interval must stay well under that idle limit.
// Tests shorten it.
var keepAliveInterval = 60 * time.Second

// progressReporter sends one progress notification. A nil reporter means the call carries no
// progress token and nothing is sent.
type progressReporter func(ctx context.Context)

// newProgressReporter returns a reporter for req, or nil when the client did not ask for progress.
// Progress is the seconds elapsed since start. Status builds the human-readable message.
//
// Delivery failures are dropped. A keep-alive is best effort, and the server's stderr must stay
// quiet because an MCP host bleeds it onto the screen.
func newProgressReporter(req *mcpsdk.CallToolRequest, start time.Time, status func(elapsed time.Duration) string) progressReporter {
	token := progressTokenOf(req)
	if token == nil {
		return nil
	}

	return func(ctx context.Context) {
		elapsed := time.Since(start)
		_ = req.Session.NotifyProgress(ctx, &mcpsdk.ProgressNotificationParams{
			ProgressToken: token,
			Progress:      elapsed.Seconds(),
			Message:       status(elapsed),
		})
	}
}

// progressTokenOf returns the progress token on req, or nil when the call cannot receive progress.
func progressTokenOf(req *mcpsdk.CallToolRequest) any {
	if req == nil || req.Params == nil || req.Session == nil {
		return nil
	}
	return req.Params.GetProgressToken()
}

// newRunReporter builds a reporter for a single run in dir. The start time comes from start.json
// so a run begun by another server reports its true age. Once start.json is gone the wait's own
// start stands in.
func newRunReporter(req *mcpsdk.CallToolRequest, dir string) progressReporter {
	if progressTokenOf(req) == nil {
		return nil
	}

	start := time.Now()
	if info, err := model.ReadStartInfo(dir); err == nil && !info.StartedAt.IsZero() {
		start = info.StartedAt
	}

	return newProgressReporter(req, start, func(elapsed time.Duration) string {
		return runStatus(dir, elapsed)
	})
}

// runStatus describes a run in flight. Stream sizes are read from disk on every call.
func runStatus(dir string, elapsed time.Duration) string {
	return fmt.Sprintf("running %s; stdout %s, stderr %s",
		model.FormatDuration(elapsed),
		streamSize(filepath.Join(dir, "stdout")),
		streamSize(filepath.Join(dir, "stderr")),
	)
}

func streamSize(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return model.FormatBytes(0)
	}
	return model.FormatBytes(info.Size())
}

// waitWithKeepAlive blocks until done closes, poll reports completion, the timeout fires, or ctx
// is cancelled. It returns true on completion and false on timeout. While it blocks, it calls
// report every keepAliveInterval.
//
// Either done or poll may be nil. A nil poll disables polling. A nil done never fires.
//
// The tickers stop before the function returns, so no notification follows the result.
func waitWithKeepAlive(ctx context.Context, done <-chan struct{}, poll func() (bool, error), timeout time.Duration, report progressReporter) (bool, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	var keepAliveC <-chan time.Time
	if report != nil {
		keepAlive := time.NewTicker(keepAliveInterval)
		defer keepAlive.Stop()
		keepAliveC = keepAlive.C
	}

	var pollC <-chan time.Time
	if poll != nil {
		pollTicker := time.NewTicker(waitPollInterval)
		defer pollTicker.Stop()
		pollC = pollTicker.C
	}

	for {
		select {
		case <-done:
			return true, nil
		case <-timer.C:
			return false, nil
		case <-ctx.Done():
			return false, ctx.Err()
		case <-keepAliveC:
			report(ctx)
		case <-pollC:
			finished, err := poll()
			if err != nil || finished {
				return finished, err
			}
		}
	}
}
