package toto

import "github.com/thediveo/enumflag/v2"

type OutputFormat enumflag.Flag

const (
	// TextOutputFormat emits prototext representation.
	TextOutputFormat OutputFormat = iota
	// JsonOutputFormat emits protojson representation.
	JsonOutputFormat
	// DebugOutputFormat emits internal go structure. It mostly looks like
	// prototext, except it does not attempt to parse certain well-known fields,
	// such as google.protobuf.Any. Such fields may be displayed as raw bytes.
	DebugOutputFormat
)

// OutputFormatOptions contains the textual value of the output format, which
// can be used in command-line flags.
var OutputFormatOptions = map[OutputFormat][]string{
	TextOutputFormat:  {"text"},
	JsonOutputFormat:  {"json"},
	DebugOutputFormat: {"debug"},
}
