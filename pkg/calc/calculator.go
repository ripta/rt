package calc

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/elk-language/go-prompt"
	"github.com/ripta/reals/pkg/constructive"
	"github.com/ripta/reals/pkg/unified"

	"github.com/ripta/rt/pkg/calc/parser"
)

var (
	ErrInvalidMetaCommand = errors.New("invalid meta-command")
	ErrInvalidMetaValue   = errors.New("invalid value")
	ErrRecursiveLoad      = errors.New("recursive .load")
	ErrInvalidFormat      = errors.New("invalid .format")
	ErrInvalidNext        = errors.New("invalid .next")
	ErrLoadStopped        = errors.New("load stopped")
)

// ExecutionMode represents the context in which an expression is being evaluated
type ExecutionMode int

const (
	// ModeREPL represents interactive REPL mode. Results are displayed and errors reported normally.
	ModeREPL ExecutionMode = iota
	// ModeSTDIN represents non-interactive mode reading from STDIN. Results are displayed and errors reported normally.
	ModeSTDIN
	// ModeLoad represents loading a saved session. Results are not displayed; errors are reported as warnings.
	ModeLoad
)

type Calculator struct {
	DecimalPlaces     int
	KeepTrailingZeros bool
	UnderscoreZeros   bool
	Verbose           bool
	Trace             bool

	count   int
	env     *parser.Env
	history []historyEntry

	// interrupt ends the line being run when the user presses Ctrl-C. Nil means never.
	interrupt context.Context

	// now supplies the timestamp .save writes. A nil now means time.Now.
	now func() time.Time

	// loading holds the files of the .load calls in progress, outermost first.
	loading []loadingFile
}

// historyEntry is an expression that ran, with the session line number it ran on.
type historyEntry struct {
	Expr string
	Line int
}

type loadingFile struct {
	name      string
	info      os.FileInfo
	numbering *loadNumbering
}

// loadNumbering is the numbering context of one .load. A nested .load gets its own, so each file's
// line numbers stay independent of the files that load it.
type loadNumbering struct {
	// version is the format version the file declares. A file with no .format is version 1.
	version int

	// headerOpen is true until the first line that is not a .format or .set line.
	headerOpen bool

	// line is the file number of the next line to process.
	line int

	// sessions maps the file number of each expression that bound a result to its session number.
	sessions map[int]int

	// first and last are the lowest and highest session numbers this load bound, when bound is true.
	first, last int
	bound       bool
}

func newLoadNumbering() *loadNumbering {
	return &loadNumbering{
		version:    minFormatVersion,
		headerOpen: true,
		sessions:   make(map[int]int),
	}
}

// session returns the session number that file line n was bound to. Version 1 files carry no line
// numbers, so none of their references map.
func (n *loadNumbering) session(line int) (int, bool) {
	if n.version < 2 {
		return 0, false
	}

	s, ok := n.sessions[line]
	return s, ok
}

func (n *loadNumbering) record(session int) {
	if !n.bound {
		n.first = session
		n.bound = true
	}

	n.last = session
}

func (n *loadNumbering) summary() string {
	switch {
	case !n.bound:
		return "no results bound"
	case n.first == n.last:
		return fmt.Sprintf("result $%d", n.first)
	default:
		return fmt.Sprintf("results $%d-$%d", n.first, n.last)
	}
}

// isStructural reports whether err leaves the numbering of the lines after it unknown. A nested
// load that stopped is not structural for the file that loaded it, unless the user cancelled it.
// A cancelled line is structural so that Ctrl-C aborts every enclosing load.
func isStructural(err error) bool {
	if errors.Is(err, context.Canceled) {
		return true
	}

	if errors.Is(err, ErrLoadStopped) {
		return false
	}

	return errors.Is(err, ErrInvalidFormat) || errors.Is(err, ErrInvalidNext)
}

// context returns the context of the line being run.
func (c *Calculator) context() context.Context {
	if c.interrupt != nil {
		return c.interrupt
	}

	return context.Background()
}

func (c *Calculator) Evaluate(expr string) (*unified.Real, error) {
	if c.env == nil {
		c.env = parser.NewEnv()
	}

	c.env.SetDecimalPlaces(c.DecimalPlaces)
	c.env.SetTrace(c.Trace)
	return Evaluate(c.context(), expr, c.env)
}

// processLine processes a single line of input (expression or meta-command).
// Returns error if processing fails.
//
// mode determines error reporting style and whether results are displayed.
// lineNum is used for error messages in ModeLoad (ignored otherwise).
func (c *Calculator) processLine(expr string, mode ExecutionMode, lineNum int) error {
	if mode == ModeLoad {
		return c.processLoadedLine(expr, lineNum)
	}

	defer func() {
		c.count++
	}()

	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil
	}

	// Handle meta-commands
	if strings.HasPrefix(expr, ".") {
		err := c.handleMetaCommand(expr)
		if err != nil {
			c.reportError(err, mode, lineNum)
		}

		return err
	}

	c.history = append(c.history, historyEntry{Expr: expr, Line: c.count})

	res, err := c.Evaluate(expr)
	if err != nil {
		c.reportError(err, mode, lineNum)
		return err
	}

	if err := c.DisplayResult(c.context(), res); err != nil {
		c.reportError(err, mode, lineNum)
		return err
	}

	c.bindResult(res)

	return nil
}

// processLoadedLine processes one non-blank line of a file being loaded. The header's .format and
// .set lines take no number. Every later line takes the next file number and the next session
// number, so loaded lines are numbered as they would be if typed at the prompt.
func (c *Calculator) processLoadedLine(expr string, lineNum int) error {
	num := c.loading[len(c.loading)-1].numbering
	expr = strings.TrimSpace(expr)

	if num.headerOpen && isHeaderLine(expr) {
		err := c.handleMetaCommand(expr)
		if err != nil && !isStructural(err) {
			c.reportError(err, ModeLoad, lineNum)
		}

		return err
	}

	num.headerOpen = false

	// A .next handler sets the line to one below its target, and this increment lands on it.
	defer func() {
		num.line++
		c.count++
	}()

	if strings.HasPrefix(expr, ".") {
		err := c.handleMetaCommand(expr)
		if err != nil && !isStructural(err) {
			c.reportError(err, ModeLoad, lineNum)
		}

		return err
	}

	rewritten, err := rewriteResultRefs(expr, num)

	var res *unified.Real
	if err == nil {
		res, err = c.Evaluate(rewritten)
	}

	if err != nil {
		// handleLoad names the interrupted line, so a warning here would repeat it.
		if !errors.Is(err, context.Canceled) {
			c.reportError(err, ModeLoad, lineNum)
		}
		return err
	}

	c.bindResult(res)
	c.history = append(c.history, historyEntry{Expr: rewritten, Line: c.count})

	num.sessions[num.line] = c.count
	num.record(c.count)

	return nil
}

// isHeaderLine reports whether expr is a .format or .set meta-command.
func isHeaderLine(expr string) bool {
	if !strings.HasPrefix(expr, ".") {
		return false
	}

	name, err := findByPrefix(strings.Fields(expr)[0], metaCommandNames)
	return err == nil && (name == ".format" || name == ".set")
}

// bindResult binds res as the constant $N for the current line number.
func (c *Calculator) bindResult(res *unified.Real) {
	c.env.SetConstant(fmt.Sprintf("$%d", c.count), res)
}

// reportError reports an error using the appropriate method for the execution mode
func (c *Calculator) reportError(err error, mode ExecutionMode, lineNum int) {
	switch mode {
	case ModeLoad:
		fmt.Fprintf(os.Stderr, "Warning: line %d: %v\n", lineNum, err)
	default:
		c.DisplayError(err)
	}
}

// Execute runs one REPL line. Ctrl-C cancels the line instead of being swallowed: go-prompt stops
// its own signal goroutine before calling the executor, and its notify registration never reads
// again.
func (c *Calculator) Execute(expr string) {
	defer fmt.Println()

	c.processInterruptible(expr, ModeREPL)
}

func (c *Calculator) DisplayError(err error) {
	fmt.Fprintf(os.Stderr, "calc:%03d/ Error: %s\n", c.count, err)
}

// DisplayResult prints res to the configured number of decimal places. A value that cannot be
// rendered, such as one outside its function's domain, is returned as an error and prints nothing
// beyond the verbose construction.
func (c *Calculator) DisplayResult(ctx context.Context, res *unified.Real) error {
	cons := res.Constructive()

	if c.Verbose {
		fmt.Printf("calc:%03d/ Construction: %s\n", c.count, constructive.AsConstruction(cons))
	}

	// Format the output to the specified number of decimal places. Insert an
	// underscore after all zeroes for readability.
	t, err := constructive.Text(ctx, cons, c.DecimalPlaces, 10)
	if err != nil {
		return fmt.Errorf("formatting result: %w", err)
	}

	if strings.Contains(t, ".") {
		if t2 := strings.TrimRight(t, "0"); len(t2) < len(t) {
			if c.UnderscoreZeros {
				t = t2 + "_" + strings.Repeat("0", len(t)-len(t2))
			} else if !c.KeepTrailingZeros {
				t = strings.TrimRight(t2, ".")
			}
		}
	}

	fmt.Printf("%s\n", t)

	return nil
}

func (c *Calculator) REPL() error {
	p := prompt.New(
		c.Execute,
		prompt.WithPrefixCallback(func() string {
			return fmt.Sprintf("calc:%03d> ", c.count)
		}),
		prompt.WithExitChecker(func(in string, breakline bool) bool {
			return breakline && (in == "exit" || in == "quit")
		}),
	)

	fmt.Printf("calc: version %s\n", version())
	fmt.Println(`calc: type an expression to calculate, ".help" for help, or ^D to exit`)
	p.Run()

	fmt.Println("calc: goodbye")
	return nil
}

// ProcessSTDIN reads expressions from STDIN and evaluates them line by line.
// This is used for non-interactive mode (e.g., piped input).
func (c *Calculator) ProcessSTDIN() error {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		c.processInterruptible(scanner.Text(), ModeSTDIN)
	}

	return scanner.Err()
}

// processInterruptible runs one line with Ctrl-C cancelling it. The handler exists only while the
// line runs, so SIGINT while blocked on input keeps its default action.
func (c *Calculator) processInterruptible(line string, mode ExecutionMode) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	c.interrupt = ctx
	defer func() {
		c.interrupt = nil
	}()

	c.processLine(line, mode, 0)
}

type metaCommandFunc func(*Calculator, []string) error

// metaCommands is the list of available meta-commands
var metaCommands map[string]metaCommandFunc

// metaCommandNames maps each meta-command name to itself, so a prefix can be resolved to a name.
var metaCommandNames map[string]string

func init() {
	metaCommands = map[string]metaCommandFunc{
		".help": func(c *Calculator, args []string) error {
			c.handleHelp()
			return nil
		},
		".examples": func(c *Calculator, args []string) error {
			c.handleExamples()
			return nil
		},
		".set": func(c *Calculator, args []string) error {
			return c.handleSet(args)
		},
		".show": func(c *Calculator, args []string) error {
			return c.handleShow(args)
		},
		".toggle": func(c *Calculator, args []string) error {
			return c.handleToggle(args)
		},
		".save": func(c *Calculator, args []string) error {
			return c.handleSave(args)
		},
		".load": func(c *Calculator, args []string) error {
			return c.handleLoad(args)
		},
		".format": func(c *Calculator, args []string) error {
			return c.handleFormat(args)
		},
		".next": func(c *Calculator, args []string) error {
			return c.handleNext(args)
		},
	}

	metaCommandNames = make(map[string]string, len(metaCommands))
	for name := range metaCommands {
		metaCommandNames[name] = name
	}
}

// handleMetaCommand routes meta-commands to handlers
func (c *Calculator) handleMetaCommand(cmd string) error {
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return fmt.Errorf("empty command")
	}

	meta, err := findByPrefix(parts[0], metaCommands)
	if err != nil {
		return err
	}

	return meta(c, parts[1:])
}

// handleSet changes a setting value
func (c *Calculator) handleSet(args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: .set <setting> <value>")
	}

	setting, err := findByPrefix(args[0], settingsRegistry)
	if err != nil {
		return err
	}

	value := args[1]

	settingName := args[0]

	switch setting.Type {
	case SettingTypeBool:
		v, err := parseBool(value)
		if err != nil {
			return fmt.Errorf("invalid value for %s: %s (use on/off, true/false, yes/no)",
				settingName, value)
		}
		setting.SetBool(c, v)
		fmt.Printf("%s %s\n", settingName, formatBool(v))

	case SettingTypeInt:
		v, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid number: %s", value)
		}
		if setting.ValidateInt != nil {
			if err := setting.ValidateInt(v); err != nil {
				return err
			}
		}
		setting.SetInt(c, v)
		fmt.Printf("%s set to %d\n", settingName, v)
	}

	return nil
}

// handleToggle toggles a boolean setting
func (c *Calculator) handleToggle(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: .toggle <setting>")
	}

	settingName := args[0]
	setting, err := findByPrefix(settingName, settingsRegistry)
	if err != nil {
		return err
	}

	if setting.Type != SettingTypeBool {
		return fmt.Errorf("cannot toggle %s: not a boolean setting", settingName)
	}

	currentValue := setting.GetBool(c)
	newValue := !currentValue
	setting.SetBool(c, newValue)
	fmt.Printf("calc:/ %s set to %s\n", settingName, formatBool(newValue))

	return nil
}

const (
	// currentFormatVersion is the session file format .save writes. A file with no .format is version 1.
	currentFormatVersion = 2
	minFormatVersion     = 1
)

// sessionFormat holds what a .format line declares.
type sessionFormat struct {
	Version int
}

// parseFormat parses the key=value arguments of .format. Unknown keys are ignored, so new metadata
// can be added without a version bump.
func parseFormat(args []string) (sessionFormat, error) {
	var format sessionFormat

	for _, arg := range args {
		key, value, ok := strings.Cut(arg, "=")
		if !ok {
			return format, fmt.Errorf("%w: expected key=value, got %q", ErrInvalidFormat, arg)
		}

		if key != "version" {
			continue
		}

		v, err := strconv.Atoi(value)
		if err != nil || v < minFormatVersion {
			return format, fmt.Errorf("%w: malformed version %q", ErrInvalidFormat, value)
		}

		if v > currentFormatVersion {
			return format, fmt.Errorf("%w: version %d is newer than the supported version %d", ErrInvalidFormat, v, currentFormatVersion)
		}

		format.Version = v
	}

	if format.Version == 0 {
		return format, fmt.Errorf("%w: missing version", ErrInvalidFormat)
	}

	return format, nil
}

// handleFormat validates a .format line. In a file being loaded, it also sets the version the
// file's lines are read with.
func (c *Calculator) handleFormat(args []string) error {
	format, err := parseFormat(args)
	if err != nil {
		return err
	}

	if len(c.loading) > 0 {
		c.loading[len(c.loading)-1].numbering.version = format.Version
	}

	return nil
}

// handleNext sets the number of the next line in the current input: the session at the prompt, or
// the file being loaded. Numbers only move forward.
func (c *Calculator) handleNext(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("%w: usage: .next <n>", ErrInvalidNext)
	}

	n, err := strconv.Atoi(args[0])
	if err != nil {
		return fmt.Errorf("%w: expected a line number, got %q", ErrInvalidNext, args[0])
	}

	current := &c.count
	if len(c.loading) > 0 {
		current = &c.loading[len(c.loading)-1].numbering.line
	}

	if n <= *current {
		return fmt.Errorf("%w: line %d is not after the current line %d", ErrInvalidNext, n, *current)
	}

	// The caller advances the line once more after this command, so stop one short.
	*current = n - 1
	return nil
}

const defaultFilename = "session.txt"

// getSessionPath resolves the session file path based on user input.
// Default: ~/.local/state/rt/calc/session.txt
//
// If arg is a directory, use default filename in that directory
// If arg is a file path, use it as-is
func getSessionPath(arg string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	if arg == "" {
		stateDir := filepath.Join(home, ".local", "state", "rt", "calc")
		if err := os.MkdirAll(stateDir, 0755); err != nil {
			return "", fmt.Errorf("failed to create state directory: %w", err)
		}

		return filepath.Join(stateDir, defaultFilename), nil
	}

	if strings.HasPrefix(arg, "~/") {
		arg = filepath.Join(home, arg[2:])
	}
	if strings.Contains(arg, "$") {
		arg = os.ExpandEnv(arg)
	}

	// If arg is an existing directory, use default filename in that directory
	if info, err := os.Stat(arg); err == nil && info.IsDir() {
		return filepath.Join(arg, defaultFilename), nil
	}

	return arg, nil
}

// handleSave saves the current session to a file
func (c *Calculator) handleSave(args []string) error {
	var argPath string
	if len(args) > 0 {
		argPath = args[0]
	}

	filename, err := getSessionPath(argPath)
	if err != nil {
		return err
	}

	f, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	defer w.Flush()

	fmt.Fprintf(w, ".format version=%d saved=%s\n", currentFormatVersion, c.timestamp().UTC().Format(time.RFC3339))

	names := make([]string, 0, len(settingsRegistry))
	for name := range settingsRegistry {
		names = append(names, name)
	}

	sort.Strings(names)

	for _, name := range names {
		setting := settingsRegistry[name]
		switch setting.Type {
		case SettingTypeBool:
			value := setting.GetBool(c)
			fmt.Fprintf(w, ".set %s %s\n", name, formatBool(value))
		case SettingTypeInt:
			value := setting.GetInt(c)
			fmt.Fprintf(w, ".set %s %d\n", name, value)
		}
	}

	if len(c.history) > 0 {
		fmt.Fprintln(w)
	}

	// The header lines are deliberately not counted, so the first entry is compared against line 0.
	want := 0
	for _, entry := range c.history {
		if entry.Line != want {
			fmt.Fprintf(w, ".next %d\n", entry.Line)
		}

		fmt.Fprintln(w, entry.Expr)
		want = entry.Line + 1
	}

	fmt.Printf("Session saved to %s\n", filename)
	return nil
}

func (c *Calculator) timestamp() time.Time {
	if c.now != nil {
		return c.now()
	}

	return time.Now()
}

// handleLoad loads a session from a file after first clearing current state.
func (c *Calculator) handleLoad(args []string) error {
	argPath := ""
	if len(args) > 0 {
		argPath = args[0]
	}

	filename, err := getSessionPath(argPath)
	if err != nil {
		return err
	}

	f, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}

	// Compare by file identity, so a symlink or another path to the same file is still caught.
	for i, lf := range c.loading {
		if os.SameFile(lf.info, info) {
			var chain []string
			for _, outer := range c.loading[i:] {
				chain = append(chain, outer.name)
			}

			chain = append(chain, filename)
			return fmt.Errorf("%w: %s", ErrRecursiveLoad, strings.Join(chain, " -> "))
		}
	}

	num := newLoadNumbering()

	c.loading = append(c.loading, loadingFile{name: filename, info: info, numbering: num})
	defer func() {
		c.loading = c.loading[:len(c.loading)-1]
	}()

	// Result history is never cleared by user actions, so only user variables and history reset. A
	// nested load is part of the load that ran it, which already reset both.
	if len(c.loading) == 1 {
		env := parser.NewEnv()
		if c.env != nil {
			env.CopyResults(c.env)
		}

		c.history = nil
		c.env = env
	}

	// The .load line keeps its own number and the loaded lines follow it. The line that ran this
	// command advances once more when it returns, so hand that one back.
	c.count++
	defer func() {
		c.count--
	}()

	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++

		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		// Expression errors are reported as warnings and don't stop loading. A structural error
		// leaves the numbering of every later line unknown.
		var err error
		if c.context().Err() != nil {
			err = parser.ErrCancelled
		} else {
			err = c.processLine(line, ModeLoad, lineNum)
		}

		if errors.Is(err, context.Canceled) {
			return fmt.Errorf("%w: interrupted at line %d of %s, keeping %s: %w", ErrLoadStopped, lineNum, filename, num.summary(), err)
		}

		if isStructural(err) {
			return fmt.Errorf("%w at line %d of %s, keeping %s: %w", ErrLoadStopped, lineNum, filename, num.summary(), err)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading file: %w", err)
	}

	fmt.Printf("Loaded from %s: %s\n", filename, num.summary())
	return nil
}

// showTopics maps each .show topic to its handler. Topics accept any
// unambiguous prefix, like every other meta-command.
var showTopics = map[string]func(*Calculator){
	"settings":  (*Calculator).showSettings,
	"functions": (*Calculator).showFunctions,
}

// handleShow displays settings or the function library, selected by an optional
// topic. No topic shows settings; a topic selects its listing by unambiguous
// prefix.
func (c *Calculator) handleShow(args []string) error {
	topic := "settings"
	if len(args) > 0 {
		topic = args[0]
	}

	show, err := findByPrefix(topic, showTopics)
	if err != nil {
		return err
	}

	show(c)
	return nil
}

// showSettings prints each setting's current value.
func (c *Calculator) showSettings() {
	fmt.Println("settings:")
	for name, setting := range settingsRegistry {
		switch setting.Type {
		case SettingTypeBool:
			fmt.Printf("  %s: %s\n", name, formatBool(setting.GetBool(c)))
		case SettingTypeInt:
			fmt.Printf("  %s: %d\n", name, setting.GetInt(c))
		}
	}
}

// showFunctions lists the registered functions grouped by category. Trig
// functions take and return radians; convert degrees explicitly, e.g.
// sin(45 * PI / 180).
func (c *Calculator) showFunctions() {
	fns := parser.Functions()

	width := 0
	for _, f := range fns {
		if len(f.Signature) > width {
			width = len(f.Signature)
		}
	}

	group := ""
	for _, f := range fns {
		if f.Group != group {
			if group != "" {
				fmt.Println()
			}
			group = f.Group
			fmt.Printf("%s:\n", group)
		}
		fmt.Printf("  %-*s  %s\n", width, f.Signature, f.Summary)
	}
}

// handleHelp displays available meta-commands
func (c *Calculator) handleHelp() {
	fmt.Println("Available commands:")
	fmt.Println("  .set <setting> <value>  - Change a setting")
	fmt.Println("  .show                   - Show current settings")
	fmt.Println("  .show functions         - List available functions")
	fmt.Println("  .toggle <setting>       - Toggle a boolean setting")
	fmt.Println("  .save [path]            - Save session (default: ~/.local/state/rt/calc/session.txt)")
	fmt.Println("  .load [path]            - Load session, keeping $N results (default: ~/.local/state/rt/calc/session.txt)")
	fmt.Println("  .next <n>               - Set the number of the next line, which must be after the current one")
	fmt.Println("  .format version=<n>     - Declare the session file format version, as the first line of a saved file")
	fmt.Println("  .examples               - Show example expressions")
	fmt.Println("  .help                   - Show this help message")
	fmt.Println()
	fmt.Println("Commands accept any unambiguous prefix, e.g., .se for .set, .sh for .show)")
	fmt.Println()
	fmt.Println("Available settings:")
	for name, setting := range settingsRegistry {
		fmt.Printf("  %-20s - %s\n", name, setting.Description)
	}
}

type example struct {
	Group string
	Expr  string
	Note  string
}

// examples are listed in order, and later entries may depend on earlier ones: `PI * r ** 2`
// uses the `r` assigned before it, and `$0` refers to the first result. Tests evaluate the
// whole list in one REPL session, so every entry must stay valid syntax.
var examples = []example{
	{"Arithmetic", "1 + 2 * 3", "precedence: * and / before + and -"},
	{"Arithmetic", "(1 + 2) * 3", "parentheses group"},
	{"Arithmetic", "22 / 7", "exact rationals, shown to the configured decimal places"},
	{"Arithmetic", "17 % 5", "modulo"},
	{"Arithmetic", "2 ** 128", "exponentiation, arbitrary precision"},
	{"Arithmetic", "1 << 8", "bit shifts: << and >>"},
	{"Arithmetic", "1_000_000 * 3", "underscores separate digits"},
	{"Arithmetic", "√2", "square root operator"},

	{"Constants", "PI * 2", "also E, PHI, SQRT2, LN2"},

	{"Functions", "sqrt(2)", "C-style calls; see .show functions"},
	{"Functions", "log(8, 2)", "multiple arguments separated by commas"},
	{"Functions", "max(3, 7, 5)", "variadic"},
	{"Functions", "sin(deg2rad(30))", "trig uses radians; nest calls to convert"},

	{"Binders", "sum(i**2, i = 1..10)", "sum over integer bounds; the index is local to the call"},
	{"Binders", "product(i, i = 1..5)", "product over integer bounds"},

	{"Variables", "r = 3", "assign a name"},
	{"Variables", "PI * r ** 2", "use it later"},
	{"Variables", "$0 + 1", "$N is the result of line N"},

	{"Comments", `"radius" 3 * 2`, "a quoted string annotates the value after it"},
}

// handleExamples prints example expressions grouped by topic.
func (c *Calculator) handleExamples() {
	width := 0
	for _, ex := range examples {
		if n := utf8.RuneCountInString(ex.Expr); n > width {
			width = n
		}
	}

	group := ""
	for _, ex := range examples {
		if ex.Group != group {
			if group != "" {
				fmt.Println()
			}
			group = ex.Group
			fmt.Printf("%s:\n", group)
		}

		pad := width - utf8.RuneCountInString(ex.Expr)
		fmt.Printf("  %s%s  # %s\n", ex.Expr, strings.Repeat(" ", pad), ex.Note)
	}
}

// parseBool parses boolean values from strings
func parseBool(s string) (bool, error) {
	switch s := strings.ToLower(s); s {
	case "on", "true", "yes", "1":
		return true, nil
	case "off", "false", "no", "0":
		return false, nil
	default:
	}

	return false, fmt.Errorf("%w: use on/off, true/false, yes/no, or 1/0", ErrInvalidMetaValue)
}

// formatBool formats boolean as on/off
func formatBool(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// version returns the current version of the calculator if set
func version() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "(devel)"
	}

	vstr := bi.Main.Version
	dirty := false
	if vstr == "(devel)" {
		for _, s := range bi.Settings {
			if s.Key == "vcs.revision" {
				vstr = s.Value
			}
			if s.Key == "vcs.modified" && s.Value == "true" {
				dirty = true
			}
		}
	}

	if dirty {
		vstr = vstr + "-dirty"
	}

	return vstr
}
