package streamdiff

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/google/cel-go/common/types/ref"
	"github.com/r3labs/diff/v3"
	"github.com/spf13/cobra"
	"github.com/thediveo/enumflag/v2"

	"github.com/ripta/rt/pkg/streamdiff/program"
	"github.com/ripta/rt/pkg/streamdiff/ui"
)

type options struct {
	KeyExpression string
	WhenFirstSeen FirstSeenMode
	InPlace       bool
}

func NewCommand() *cobra.Command {
	o := options{
		KeyExpression: "[obj.kind, obj.metadata.name]",
		// KeyExpression: `obj.kind + "/" + obj.metadata.name`,
		WhenFirstSeen: EmitKeysOnFirstSeen,
	}
	c := cobra.Command{
		Use:           "streamdiff",
		Short:         "View a diff of streaming JSON",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			return run(&o)
		},
	}

	efs := enumflag.New(&o.WhenFirstSeen, "seen-mode", FirstSeenModeOptions, enumflag.EnumCaseInsensitive)
	c.Flags().VarP(efs, "seen-mode", "s", "What to do when an object is first seen: keys-only, silence, or full")
	c.Flags().BoolVarP(&o.InPlace, "in-place", "i", false, "Update and show in place")

	return &c
}

func run(o *options) error {
	lines := 0
	uniques := 0
	r := os.Stdin

	keyPrg, err := program.New("obj", o.KeyExpression)
	if err != nil {
		return fmt.Errorf("compiling key expression: %w", err)
	}

	history := map[string]any{}
	viewer := (ui.Viewer)(nil)

	if o.InPlace {
		ts := ui.NewThrobberSet([]string{"-", "\\", "|", "/"})
		ipv, closer, err := ui.NewInPlaceView(os.Stdout, ts, 150*time.Millisecond)
		if err != nil {
			return err
		}

		viewer = ipv
		defer closer()
	} else {
		viewer = ui.NewTerminalView(os.Stdout)
	}

	d := json.NewDecoder(r)
	for d.More() {
		lines++

		var obj interface{}
		if err := d.Decode(&obj); err != nil {
			return fmt.Errorf("decoding input: %w", err)
		}

		out, _, err := keyPrg.Run(obj)
		if err != nil {
			return NewProgramEvaluationError(err, o.KeyExpression, obj)
		}

		key, err := asKey(out)
		// fmt.Fprintf(os.Stderr, "%s\n", key)
		if prev, ok := history[key]; ok {
			changes, err := diff.Diff(prev, obj)
			if err != nil {
				return fmt.Errorf("calculating diff: %w", err)
			}

			changes = changes.FilterOut([]string{"^metadata$", "^resourceVersion$"})
			changes = changes.FilterOut([]string{"^status$", "^conditions$", ".+", "^lastHeartbeatTime$"})

			// conditionChanges := changes.Filter([]string{"^status$", "^conditions$", ".+", "^lastHeartbeatTime$"})
			// for _, conditionChange := range conditionChanges {
			// 	strconv.Itoa(conditionChange.Path[2])
			// }

			viewer.Update(key, changes)

			// res := map[string]any{}
			// diff.Patch(changes, &res)
			// fmt.Fprintf(os.Stdout, "%+v\n", res)
		} else {
			viewer.Update(key, nil)
		}
		history[key] = obj

		uniques++
	}

	fmt.Fprintf(os.Stderr, "Uniques: %d; Lines: %d\n", uniques, lines)
	return nil
}

var (
	stringType      = reflect.TypeOf("")
	stringSliceType = reflect.TypeOf([]string{""})
)

func asKey(val ref.Val) (string, error) {
	if ssi, err := val.ConvertToNative(stringSliceType); err == nil {
		return strings.Join(ssi.([]string), ":"), nil
	} else if si, err := val.ConvertToNative(stringType); err == nil {
		return si.(string), nil
	} else {
		bs, err := json.Marshal(val)
		return string(bs), err
	}
}
