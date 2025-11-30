package calc

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/elk-language/go-prompt"
	"github.com/ripta/reals/pkg/constructive"
	"github.com/ripta/reals/pkg/unified"

	"github.com/ripta/rt/pkg/calc/parser"
)

var (
	ErrInvalidMetaCommand = errors.New("invalid meta-command")
	ErrInvalidMetaValue   = errors.New("invalid value")
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
	history []string
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
		c.history = append(c.history, expr)
	}

	res, err := c.Evaluate(expr)
	if err != nil {
		c.reportError(err, mode, lineNum)
		return err
	}

	// ModeLoad adds to history after successful evaluation
	if mode == ModeLoad {
		c.history = append(c.history, expr)
	}

	// Display results (except in Load mode)
	if mode != ModeLoad {
		c.DisplayResult(res)
	}

	return nil
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
		".set": func(c *Calculator, args []string) error {
			return c.handleSet(args)
		},
		".show": func(c *Calculator, args []string) error {
			c.handleShow()
			return nil
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

	fmt.Fprintf(w, "# Calculator Session\n")
	fmt.Fprintf(w, "# Saved: %s\n\n", time.Now().Format("2006-01-02 15:04:05"))

	// Write settings as .set commands
	for name, setting := range settingsRegistry {
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

	// Write expression history
	for _, expr := range c.history {
		fmt.Fprintln(w, expr)
	}

	fmt.Printf("Session saved to %s\n", filename)
	return nil
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

	c.history = nil
	c.env = parser.NewEnv()

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

// handleShow displays current settings
func (c *Calculator) handleShow() {
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

// handleHelp displays available meta-commands
func (c *Calculator) handleHelp() {
	fmt.Println("Available commands:")
	fmt.Println("  .set <setting> <value>  - Change a setting")
	fmt.Println("  .show                   - Show current settings")
	fmt.Println("  .toggle <setting>       - Toggle a boolean setting")
	fmt.Println("  .save [path]            - Save session (default: ~/.local/state/rt/calc/session.txt)")
	fmt.Println("  .load [path]            - Load session (default: ~/.local/state/rt/calc/session.txt)")
	fmt.Println("  .help                   - Show this help message")
	fmt.Println()
	fmt.Println("Commands accept any unambiguous prefix, e.g., .se for .set, .sh for .show)")
	fmt.Println()
	fmt.Println("Available settings:")
	for name, setting := range settingsRegistry {
		fmt.Printf("  %-20s - %s\n", name, setting.Description)
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
