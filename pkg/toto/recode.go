package toto

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/thediveo/enumflag/v2"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
)

func newRecodeCommand(t *options) *cobra.Command {
	cmd := &cobra.Command{
		Use: "recode",
		RunE: func(_ *cobra.Command, args []string) error {
			if len(args) != 1 {
				return fmt.Errorf("expecting 1 message type as argument, but received %d", len(args))
			}

			return runRecode(t, args[0])
		},
	}

	cmd.PersistentFlags().StringVarP(&t.Path, "path", "p", t.Path, "Path to file descriptor set")

	// Output format
	f := enumflag.New(&t.Format, "format", OutputFormatOptions, enumflag.EnumCaseInsensitive)
	cmd.Flags().VarP(f, "format", "f", "Output format, one of: text, json, debug")

	// Options for JSON and text output formats
	cmd.Flags().BoolVar(&t.AllowPartial, "accept-partial", false, "For JSON & text: Accept messages that have missing required fields")

	// Options for JSON output format
	cmd.Flags().BoolVar(&t.UseProtoNames, "use-proto-names", false, "For JSON: Use the field names defined in the proto file instead of lowerCamelCase names")
	cmd.Flags().BoolVar(&t.UseEnumNumbers, "use-enum-numbers", false, "For JSON: Use numbers instead of string as enum values")
	cmd.Flags().BoolVar(&t.EmitUnpopulated, "emit-unpopulated", false, "For JSON: Emit unpopulated fields (excluding oneof and extension fields)")

	// Options for text output format
	cmd.Flags().BoolVar(&t.EmitASCII, "emit-ascii", false, "For text: Emit ASCII instead of UTF-8")
	cmd.Flags().BoolVar(&t.EmitUnknown, "emit-unknown", false, "For text: Emit unknown fields")

	return cmd
}

func runRecode(t *options, mt string) error {
	fdsPath := t.Path
	fi, err := os.Stat(fdsPath)
	if err != nil {
		return err
	}

	if fi.IsDir() {
		fdsPath = filepath.Join(fdsPath, ".file_descriptor_set")
	}

	r, err := os.Open(fdsPath)
	if err != nil {
		return err
	}

	fds, err := LoadImage(r)
	if err != nil {
		return err
	}

	reg, err := protodesc.NewFiles(fds)
	if err != nil {
		return fmt.Errorf("could not initialize dynamic registry: %w", err)
	}

	resolver, err := ConvertFilesToTypes(reg)
	if err != nil {
		return fmt.Errorf("could not create type registry: %w", err)
	}

	desc, err := reg.FindDescriptorByName(protoreflect.FullName(mt))
	if err != nil {
		return fmt.Errorf("could not find descriptor %q: %w", mt, err)
	}

	md, ok := desc.(protoreflect.MessageDescriptor)
	if !ok {
		return fmt.Errorf("could not instantiate message: %s is %T, not a protoreflect.MessageDescriptor", mt, desc)
	}

	decodeOpts := proto.UnmarshalOptions{
		RecursionLimit: protowire.DefaultRecursionLimit,
		Resolver:       resolver,
	}

	bs, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("could not read from stdin: %w", err)
	}

	pm := dynamicpb.NewMessage(md)
	if err := decodeOpts.Unmarshal(bs, pm); err != nil {
		return fmt.Errorf("could not unmarshal message from stdin: %w", err)
	}

	switch t.Format {
	case TextOutputFormat:
		textOpts := prototext.MarshalOptions{
			Multiline: true,
			Indent:    "  ",
			Resolver:  resolver,
		}

		out, err := textOpts.Marshal(pm)
		if err != nil {
			return fmt.Errorf("could not marshal message back to text: %w", err)
		}

		fmt.Println(string(out))
	case JsonOutputFormat:
		jsonOpts := protojson.MarshalOptions{
			Multiline: true,
			Indent:    "  ",
			Resolver:  resolver,
		}

		out, err := jsonOpts.Marshal(pm)
		if err != nil {
			return fmt.Errorf("could not marshal message back to json: %w", err)
		}

		fmt.Println(string(out))
	case DebugOutputFormat:
		fmt.Printf("%+v\n", pm)
	default:
		return errors.New("invalid format option")
	}

	return nil
}
