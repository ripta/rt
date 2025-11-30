package calc

import (
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

func (c *Calculator) REPL() {
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
}

// handleMetaCommand routes meta-commands to handlers
func (c *Calculator) handleMetaCommand(cmd string) error {
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return fmt.Errorf("empty command")
	}

	switch parts[0] {
	case ".set":
		return c.handleSet(parts[1:])
	case ".show":
		c.handleShow()
		return nil
	case ".help":
		c.handleHelp()
		return nil
	default:
	}

	return fmt.Errorf("%w: %s", ErrInvalidMetaCommand, parts[0])
}

// handleSet changes a setting value
func (c *Calculator) handleSet(args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: .set <setting> <value>")
	}

	setting := strings.ToLower(args[0])
	value := args[1]

	switch setting {
	case "trace":
		v, err := parseBool(value)
		if err != nil {
			return fmt.Errorf("invalid value for trace: %s (use on/off, true/false, yes/no)", value)
		}
		c.Trace = v
		fmt.Printf("Trace mode %s\n", formatBool(v))

	case "decimal_places":
		v, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid number: %s", value)
		}
		if v < 0 {
			return fmt.Errorf("decimal_places must be non-negative")
		}
		c.DecimalPlaces = v
		fmt.Printf("Decimal places set to %d\n", v)

	case "keep_trailing_zeros":
		v, err := parseBool(value)
		if err != nil {
			return fmt.Errorf("invalid value for keep_trailing_zeros: %s (use on/off, true/false, yes/no)", value)
		}
		c.KeepTrailingZeros = v
		fmt.Printf("Keep trailing zeros %s\n", formatBool(v))

	case "underscore_zeros":
		v, err := parseBool(value)
		if err != nil {
			return fmt.Errorf("invalid value for underscore_zeros: %s (use on/off, true/false, yes/no)", value)
		}
		c.UnderscoreZeros = v
		fmt.Printf("Underscore zeros %s\n", formatBool(v))

	case "verbose":
		v, err := parseBool(value)
		if err != nil {
			return fmt.Errorf("invalid value for verbose: %s (use on/off, true/false, yes/no)", value)
		}
		c.Verbose = v
		fmt.Printf("Verbose mode %s\n", formatBool(v))

	default:
		return fmt.Errorf("unknown setting: %s", setting)
	}

	return nil
}

// handleShow displays current settings
func (c *Calculator) handleShow() {
	fmt.Println("settings:")
	fmt.Printf("  trace: %s\n", formatBool(c.Trace))
	fmt.Printf("  decimal_places: %d\n", c.DecimalPlaces)
	fmt.Printf("  keep_trailing_zeros: %s\n", formatBool(c.KeepTrailingZeros))
	fmt.Printf("  underscore_zeros: %s\n", formatBool(c.UnderscoreZeros))
	fmt.Printf("  verbose: %s\n", formatBool(c.Verbose))
}

// handleHelp displays available meta-commands
func (c *Calculator) handleHelp() {
	fmt.Println("Available commands:")
	fmt.Println("  .set <setting> <value>  - Change a setting")
	fmt.Println("  .show                   - Show current settings")
	fmt.Println("  .help                   - Show this help message")
	fmt.Println()
	fmt.Println("Available settings:")
	fmt.Println("  trace               - Enable/disable trace output (on/off)")
	fmt.Println("  decimal_places      - Number of decimal places to display (integer)")
	fmt.Println("  keep_trailing_zeros - Keep trailing zeros in output (on/off)")
	fmt.Println("  underscore_zeros    - Insert underscore before trailing zeros (on/off)")
	fmt.Println("  verbose             - Enable verbose output (on/off)")
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
