package streamdiff

import "github.com/thediveo/enumflag/v2"

type FirstSeenMode enumflag.Flag

const (
	EmitKeysOnFirstSeen FirstSeenMode = iota
	SilenceOnFirstSeen
	FullObjectOnFirstSeen
)

var FirstSeenModeOptions = map[FirstSeenMode][]string{
	EmitKeysOnFirstSeen:   {"keys-only", "keys"},
	SilenceOnFirstSeen:    {"silence", "quiet"},
	FullObjectOnFirstSeen: {"full", "all"},
}
