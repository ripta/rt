package model

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// History log file names within the history directory.
const (
	HistoryLogFilename     = "history.jsonl"
	HistoryRotatedFilename = "history.1.jsonl"
	HistoryKeyFilename     = "history.key"
	HistoryLockFilename    = "history.lock"
)

// HistoryVersion is the "v" every record written today carries. Readers must skip records
// with a version or kind they do not know, so a later format can share the file.
const HistoryVersion = 1

// Record kinds. A reader skips kinds it does not know.
const (
	HistoryKindRun  = "run"
	HistoryKindPool = "pool"
)

// HistoryMaxArgvMACs bounds the prefix chain in a run record. An argv with more tokens also
// gets the MAC of its full argv as a final entry, so exact matches keep working.
const HistoryMaxArgvMACs = 32

// historyMaxBytes is the size past which the log rotates to the older generation.
const historyMaxBytes = 8 << 20

const historyKeyBytes = 32

// historyMACBytes is the MAC length kept after truncation.
const historyMACBytes = 16

// RunHistoryRecord is the history line for one finished run. Argv and cwd appear only as
// keyed MACs, never as plaintext. Parallelism is 0 for a standalone run.
type RunHistoryRecord struct {
	V           int       `json:"v"`
	Kind        string    `json:"kind"`
	ID          string    `json:"id"`
	StartedAt   time.Time `json:"started_at"`
	DurationMs  int64     `json:"duration_ms"`
	ExitCode    int       `json:"exit_code"`
	Signal      *int      `json:"signal"`
	Pool        bool      `json:"pool"`
	Parallelism int       `json:"parallelism"`
	CwdMAC      string    `json:"cwd_mac"`
	ArgvMACs    []string  `json:"argv_macs"`
}

// PoolHistoryRecord is the history line for one finished pool. DurationMs is the pool's
// wall time. OK is true when every scheduled run finished with exit code 0 and no signal.
type PoolHistoryRecord struct {
	V           int       `json:"v"`
	Kind        string    `json:"kind"`
	ID          string    `json:"id"`
	StartedAt   time.Time `json:"started_at"`
	DurationMs  int64     `json:"duration_ms"`
	Parallelism int       `json:"parallelism"`
	OK          bool      `json:"ok"`
	CwdMAC      string    `json:"cwd_mac"`
	PoolMAC     string    `json:"pool_mac"`
}

// History is a handle on the durable history log: its directory and the MAC key. Records
// hold only keyed hashes, so the log survives `cg prune` and TMPDIR cleanup without keeping
// secrets that argv may carry.
type History struct {
	dir      string
	key      []byte
	maxBytes int64
}

// HistoryDir returns the history directory: $XDG_STATE_HOME/cg when that is an absolute
// path, else ~/.local/state/cg. The fallback is built from the home directory explicitly.
// That keeps it at ~/.local/state on macOS.
func HistoryDir() (string, error) {
	if base := os.Getenv("XDG_STATE_HOME"); filepath.IsAbs(base) {
		return filepath.Join(base, "cg"), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}

	return filepath.Join(home, ".local", "state", "cg"), nil
}

// OpenHistory prepares the history directory and loads the MAC key, creating both on first
// use. Concurrent first users agree on one key.
func OpenHistory() (*History, error) {
	dir, err := HistoryDir()
	if err != nil {
		return nil, err
	}

	return openHistoryAt(dir)
}

func openHistoryAt(dir string) (*History, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("creating history dir: %w", err)
	}
	if err := checkOwnedDir(dir); err != nil {
		return nil, err
	}

	key, err := loadHistoryKey(dir)
	if err != nil {
		return nil, err
	}

	return &History{dir: dir, key: key, maxBytes: historyMaxBytes}, nil
}

// Dir returns the history directory.
func (h *History) Dir() string {
	return h.dir
}

// LogPaths returns the log generations, oldest first.
func (h *History) LogPaths() []string {
	return []string{
		filepath.Join(h.dir, HistoryRotatedFilename),
		filepath.Join(h.dir, HistoryLogFilename),
	}
}

func loadHistoryKey(dir string) ([]byte, error) {
	path := filepath.Join(dir, HistoryKeyFilename)

	key, err := readHistoryKey(path)
	if err == nil {
		return key, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	fresh := make([]byte, historyKeyBytes)
	if _, err := rand.Read(fresh); err != nil {
		return nil, fmt.Errorf("generating history key: %w", err)
	}

	// The key is written whole to a temp file and then linked into place. A reader that
	// loses the race to create it never sees a partial key, and link fails with EEXIST when
	// another creator got there first.
	tmp, err := os.CreateTemp(dir, HistoryKeyFilename+".tmp-*")
	if err != nil {
		return nil, fmt.Errorf("creating history key: %w", err)
	}
	defer os.Remove(tmp.Name())

	_, werr := tmp.Write(fresh)
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return nil, fmt.Errorf("writing history key: %w", werr)
	}

	if err := os.Link(tmp.Name(), path); err != nil {
		if errors.Is(err, os.ErrExist) {
			return readHistoryKey(path)
		}
		return nil, fmt.Errorf("installing history key: %w", err)
	}

	return fresh, nil
}

func readHistoryKey(path string) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if errors.Is(err, syscall.ELOOP) {
		return nil, fmt.Errorf("%s: %w", path, ErrNotRegularFile)
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: %w", path, ErrNotRegularFile)
	}

	key, err := io.ReadAll(io.LimitReader(f, historyKeyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading history key: %w", err)
	}
	if len(key) != historyKeyBytes {
		return nil, fmt.Errorf("%s: history key must be %d bytes", path, historyKeyBytes)
	}

	return key, nil
}

// ArgvKey joins an argv into one string with NUL separators. Argv tokens cannot contain NUL,
// so the join is collision-free.
func ArgvKey(argv []string) string {
	return strings.Join(argv, "\x00")
}

// MAC returns the truncated, base64url-encoded HMAC-SHA256 of data under the history key.
func (h *History) MAC(data string) string {
	m := hmac.New(sha256.New, h.key)
	m.Write([]byte(data))

	return base64.RawURLEncoding.EncodeToString(m.Sum(nil)[:historyMACBytes])
}

// ArgvMAC returns the MAC of the full argv.
func (h *History) ArgvMAC(argv []string) string {
	return h.MAC(ArgvKey(argv))
}

// ArgvMACs returns the MAC chain for argv. Entry k is the MAC of the first k+1 tokens. An
// argv longer than HistoryMaxArgvMACs stops the prefix chain there and adds the full-argv MAC
// as a final entry.
func (h *History) ArgvMACs(argv []string) []string {
	n := min(len(argv), HistoryMaxArgvMACs)
	macs := make([]string, 0, n+1)
	for k := 1; k <= n; k++ {
		macs = append(macs, h.ArgvMAC(argv[:k]))
	}
	if len(argv) > HistoryMaxArgvMACs {
		macs = append(macs, h.ArgvMAC(argv))
	}

	return macs
}

// CwdMAC returns the MAC of cwd after resolving symlinks, so a path reached through a link
// and its target hash the same. A path that cannot be resolved is hashed as given.
func (h *History) CwdMAC(cwd string) string {
	if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = resolved
	}

	return h.MAC(cwd)
}

// PoolMAC returns the MAC identifying a pool shape: each command's full-argv MAC in listed
// order, then repeat and parallelism as decimal strings, all NUL-joined. The argv MACs are
// fixed width and hold no NUL, so pools that split the same tokens into different commands
// cannot collide.
func (h *History) PoolMAC(commands [][]string, repeat, parallelism int) string {
	parts := make([]string, 0, len(commands)+2)
	for _, argv := range commands {
		parts = append(parts, h.ArgvMAC(argv))
	}
	parts = append(parts, strconv.Itoa(repeat), strconv.Itoa(parallelism))

	return h.MAC(strings.Join(parts, "\x00"))
}

// NewRunRecord builds the history record for a finished run. parallelism is the pool's, or 0
// for a standalone run.
func (h *History) NewRunRecord(m *Meta, parallelism int) *RunHistoryRecord {
	return &RunHistoryRecord{
		V:           HistoryVersion,
		Kind:        HistoryKindRun,
		ID:          m.ID,
		StartedAt:   m.StartedAt.UTC(),
		DurationMs:  m.DurationMs,
		ExitCode:    m.ExitCode,
		Signal:      m.Signal,
		Pool:        m.Pool != "",
		Parallelism: parallelism,
		CwdMAC:      h.CwdMAC(m.Cwd),
		ArgvMACs:    h.ArgvMACs(m.Command),
	}
}

// NewPoolRecord builds the history record for a finished pool. It returns false when the
// manifest is not finished.
func (h *History) NewPoolRecord(m *PoolManifest) (*PoolHistoryRecord, bool) {
	if m.FinishedAt == nil {
		return nil, false
	}

	counts := m.Counts()

	return &PoolHistoryRecord{
		V:           HistoryVersion,
		Kind:        HistoryKindPool,
		ID:          m.ID,
		StartedAt:   m.StartedAt.UTC(),
		DurationMs:  m.FinishedAt.Sub(m.StartedAt).Milliseconds(),
		Parallelism: m.Parallelism,
		OK:          counts.Total > 0 && counts.Succeeded == counts.Total,
		CwdMAC:      h.CwdMAC(m.Cwd),
		PoolMAC:     h.PoolMAC(m.Commands, m.Repeat, m.Parallelism),
	}, true
}

// Append writes one record as one whole line. The append runs under an exclusive flock on
// history.lock. Concurrent appenders produce whole lines, and rotation cannot race an append.
func (h *History) Append(record any) error {
	line, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encoding history record: %w", err)
	}
	line = append(line, '\n')

	lock, err := openHistoryFile(filepath.Join(h.dir, HistoryLockFilename), os.O_CREATE|os.O_RDWR)
	if err != nil {
		return fmt.Errorf("opening history lock: %w", err)
	}
	defer lock.Close()

	if err := flockExclusive(lock); err != nil {
		return fmt.Errorf("locking history: %w", err)
	}

	logPath := filepath.Join(h.dir, HistoryLogFilename)
	if info, err := os.Lstat(logPath); err == nil && info.Size() > h.maxBytes {
		if err := os.Rename(logPath, filepath.Join(h.dir, HistoryRotatedFilename)); err != nil {
			return fmt.Errorf("rotating history: %w", err)
		}
	}

	f, err := openHistoryFile(logPath, os.O_WRONLY|os.O_APPEND|os.O_CREATE)
	if err != nil {
		return fmt.Errorf("opening history log: %w", err)
	}

	_, werr := f.Write(line)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return fmt.Errorf("writing history: %w", werr)
	}

	return nil
}

// openHistoryFile opens a file in the history directory, refusing symlinks and anything that
// is not a regular file. O_NONBLOCK keeps a planted FIFO from hanging the open, and it has no
// effect on regular files.
func openHistoryFile(path string, flags int) (*os.File, error) {
	f, err := os.OpenFile(path, flags|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0o600)
	if errors.Is(err, syscall.ELOOP) {
		return nil, fmt.Errorf("%s: %w", path, ErrNotRegularFile)
	}
	if err != nil {
		return nil, err
	}

	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() {
		f.Close()
		return nil, fmt.Errorf("%s: %w", path, ErrNotRegularFile)
	}

	return f, nil
}

// flockExclusive blocks until it holds an exclusive lock on f.
func flockExclusive(f *os.File) error {
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
		if !errors.Is(err, syscall.EINTR) {
			return err
		}
	}
}

// AppendRunHistory records a finished run in the history log.
func AppendRunHistory(m *Meta, parallelism int) error {
	h, err := OpenHistory()
	if err != nil {
		return err
	}

	return h.Append(h.NewRunRecord(m, parallelism))
}

// AppendPoolHistory records a finished pool in the history log.
func AppendPoolHistory(m *PoolManifest) error {
	h, err := OpenHistory()
	if err != nil {
		return err
	}

	record, ok := h.NewPoolRecord(m)
	if !ok {
		return nil
	}

	return h.Append(record)
}
