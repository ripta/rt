package calc

import (
	"bufio"
	"errors"
	"fmt"
	"os"
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
	name string
	info os.FileInfo
}

func (c *Calculator) Evaluate(expr string) (*unified.Real, error) {
	if c.env == nil {
		c.env = parser.NewEnv()
	}

	c.env.SetDecimalPlaces(c.DecimalPlaces)
	c.env.SetTrace(c.Trace)
	return Evaluate(expr, c.env)
}

// processLine processes a single line of input (expression or meta-command).
// Returns error if processing fails.
//
// mode determines error reporting style and whether results are displayed.
// lineNum is used for error messages in ModeLoad (ignored otherwise).
func (c *Calculator) processLine(expr string, mode ExecutionMode, lineNum int) error {
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

	// ModeREPL and ModeSTDIN adds to history before evaluation
	if mode == ModeREPL || mode == ModeSTDIN {
		c.history = append(c.history, historyEntry{Expr: expr, Line: c.count})
	}

	res, err := c.Evaluate(expr)
	if err != nil {
		c.reportError(err, mode, lineNum)
		return err
	}

	if mode == ModeREPL || mode == ModeSTDIN {
		c.bindResult(res)
	}

	// ModeLoad adds to history after successful evaluation
	if mode == ModeLoad {
		c.history = append(c.history, historyEntry{Expr: expr, Line: c.count})
	}

	// Display results (except in Load mode)
	if mode != ModeLoad {
		c.DisplayResult(res)
	}

	return nil
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

func (c *Calculator) Execute(expr string) {
	defer fmt.Println()
	c.processLine(expr, ModeREPL, 0)
}

func (c *Calculator) DisplayError(err error) {
	fmt.Fprintf(os.Stderr, "calc:%03d/ Error: %s\n", c.count, err)
}

func (c *Calculator) DisplayResult(res *unified.Real) {
	cons := res.Constructive()

	if c.Verbose {
		fmt.Printf("calc:%03d/ Construction: %s\n", c.count, constructive.AsConstruction(cons))
	}

	// Format the output to the specified number of decimal places. Insert an
	// underscore after all zeroes for readability.
	t := constructive.Text(cons, c.DecimalPlaces, 10)
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
		line := scanner.Text()
		c.processLine(line, ModeSTDIN, 0)
	}

	return scanner.Err()
}

type metaCommandFunc func(*Calculator, []string) error

// metaCommands is the list of available meta-commands
var metaCommands map[string]metaCommandFunc

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
			_, err := parseFormat(args)
			return err
		},
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

	c.loading = append(c.loading, loadingFile{name: filename, info: info})
	defer func() {
		c.loading = c.loading[:len(c.loading)-1]
	}()

	// Result history is never cleared by user actions, so only user variables and history reset.
	env := parser.NewEnv()
	if c.env != nil {
		env.CopyResults(c.env)
	}

	c.history = nil
	c.env = env

	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++

		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		// Errors are reported as warnings but don't stop loading
		c.processLine(line, ModeLoad, lineNum)
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading file: %w", err)
	}

	fmt.Printf("Loaded from %s\n", filename)
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
