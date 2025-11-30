package calc

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"runtime/debug"
	"strconv"
	"strings"

	"github.com/elk-language/go-prompt"
	"github.com/ripta/reals/pkg/constructive"
	"github.com/ripta/reals/pkg/unified"

	"github.com/ripta/rt/pkg/calc/parser"
)

var (
	ErrInvalidMetaCommand = errors.New("invalid meta-command")
	ErrInvalidMetaValue   = errors.New("invalid value")
)

type Calculator struct {
	DecimalPlaces     int
	KeepTrailingZeros bool
	UnderscoreZeros   bool
	Verbose           bool
	Trace             bool

	count int
	env   *parser.Env
}

func (c *Calculator) Evaluate(expr string) (*unified.Real, error) {
	if c.env == nil {
		c.env = parser.NewEnv()
	}

	c.env.SetDecimalPlaces(c.DecimalPlaces)
	c.env.SetTrace(c.Trace)
	return Evaluate(expr, c.env)
}

func (c *Calculator) Execute(expr string) {
	defer func() {
		c.count++
		fmt.Println()
	}()

	expr = strings.TrimSpace(expr)
	if strings.HasPrefix(expr, ".") {
		if err := c.handleMetaCommand(expr); err != nil {
			c.DisplayError(err)
		}
		return
	}

	res, err := c.Evaluate(expr)
	if err != nil {
		c.DisplayError(err)
		return
	}

	c.DisplayResult(res)
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
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, ".") {
			if err := c.handleMetaCommand(line); err != nil {
				c.DisplayError(err)
			}
			c.count++
			continue
		}

		res, err := c.Evaluate(line)
		if err != nil {
			c.DisplayError(err)
			c.count++
			continue
		}

		c.DisplayResult(res)
		c.count++
	}

	return scanner.Err()
}

type metaCommandFunc func(*Calculator, []string) error

// metaCommands is the list of available meta-commands
var metaCommands = map[string]metaCommandFunc{
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
}

// findMetaCommand finds a meta-command by prefix matching. Returns the command
// function if exactly one match is found. Returns error if no matches or
// multiple (ambiguous) matches.
func findMetaCommand(prefix string) (metaCommandFunc, error) {
	var matches []string
	for cmd := range metaCommands {
		if strings.HasPrefix(cmd, prefix) {
			matches = append(matches, cmd)
		}
	}

	if len(matches) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrInvalidMetaCommand, prefix)
	} else if len(matches) > 1 {
		return nil, fmt.Errorf("ambiguous command %q, could be one of: %s", prefix, strings.Join(matches, ", "))
	}

	return metaCommands[matches[0]], nil
}

// handleMetaCommand routes meta-commands to handlers
func (c *Calculator) handleMetaCommand(cmd string) error {
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return fmt.Errorf("empty command")
	}

	meta, err := findMetaCommand(parts[0])
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

	setting, err := findSetting(args[0])
	if err != nil {
		return err
	}

	value := args[1]

	switch setting.Type {
	case SettingTypeBool:
		v, err := parseBool(value)
		if err != nil {
			return fmt.Errorf("invalid value for %s: %s (use on/off, true/false, yes/no)",
				setting.Name, value)
		}
		setting.SetBool(c, v)
		fmt.Printf("%s %s\n", formatSettingName(setting.Name), formatBool(v))

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
		fmt.Printf("%s set to %d\n", formatSettingName(setting.Name), v)
	}

	return nil
}

// handleToggle toggles a boolean setting
func (c *Calculator) handleToggle(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: .toggle <setting>")
	}

	setting, err := findSetting(args[0])
	if err != nil {
		return err
	}

	if setting.Type != SettingTypeBool {
		return fmt.Errorf("cannot toggle %s: not a boolean setting", setting.Name)
	}

	currentValue := setting.GetBool(c)
	newValue := !currentValue
	setting.SetBool(c, newValue)
	fmt.Printf("%s %s\n", formatSettingName(setting.Name), formatBool(newValue))

	return nil
}

// handleShow displays current settings
func (c *Calculator) handleShow() {
	fmt.Println("settings:")
	for _, setting := range settingsRegistry {
		switch setting.Type {
		case SettingTypeBool:
			fmt.Printf("  %s: %s\n", setting.Name, formatBool(setting.GetBool(c)))
		case SettingTypeInt:
			fmt.Printf("  %s: %d\n", setting.Name, setting.GetInt(c))
		}
	}
}

// handleHelp displays available meta-commands
func (c *Calculator) handleHelp() {
	fmt.Println("Available commands:")
	fmt.Println("  .set <setting> <value>  - Change a setting")
	fmt.Println("  .show                   - Show current settings")
	fmt.Println("  .toggle <setting>       - Toggle a boolean setting")
	fmt.Println("  .help                   - Show this help message")
	fmt.Println()
	fmt.Println("Commands accept any unambiguous prefix, e.g., .se for .set, .sh for .show)")
	fmt.Println()
	fmt.Println("Available settings:")
	for _, setting := range settingsRegistry {
		fmt.Printf("  %-20s - %s\n", setting.Name, setting.Description)
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
