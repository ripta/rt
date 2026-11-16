package model

import (
	"encoding/binary"
	"slices"
	"testing"
)

// procargs2 builds a kern.procargs2 buffer: a native-endian argc, the executable path, padding NULs,
// then the arguments, each NUL-terminated.
func procargs2(argc int32, exe string, padding int, args ...string) []byte {
	buf := binary.NativeEndian.AppendUint32(nil, uint32(argc))
	buf = append(buf, exe...)
	buf = append(buf, 0)
	for range padding {
		buf = append(buf, 0)
	}
	for _, a := range args {
		buf = append(buf, a...)
		buf = append(buf, 0)
	}
	return buf
}

type procArgs2Test struct {
	Name    string
	Raw     []byte
	Want    []string
	WantErr bool
}

var procArgs2Tests = []procArgs2Test{
	{Name: "arguments", Raw: procargs2(3, "/bin/cg", 3, "cg", "supervise-run", "/tmp/x", "HOME=/h"), Want: []string{"cg", "supervise-run", "/tmp/x"}},
	{Name: "no padding", Raw: procargs2(1, "/bin/cg", 0, "cg"), Want: []string{"cg"}},
	{Name: "environment ignored", Raw: procargs2(1, "/bin/cg", 1, "cg", "A=1", "B=2"), Want: []string{"cg"}},
	{Name: "zero argc", Raw: procargs2(0, "/bin/cg", 1), Want: []string{}},
	{Name: "short buffer", Raw: []byte{1, 0}, WantErr: true},
	{Name: "empty buffer", Raw: nil, WantErr: true},
	{Name: "unterminated path", Raw: append(binary.NativeEndian.AppendUint32(nil, 1), "/bin/cg"...), WantErr: true},
	{Name: "fewer args than argc", Raw: procargs2(3, "/bin/cg", 1, "cg", "supervise-run"), WantErr: true},
	{Name: "negative argc", Raw: procargs2(-1, "/bin/cg", 1, "cg"), WantErr: true},
	{Name: "huge argc", Raw: procargs2(1<<30, "/bin/cg", 1, "cg"), WantErr: true},
}

func TestParseProcArgs2(t *testing.T) {
	for _, tt := range procArgs2Tests {
		t.Run(tt.Name, func(t *testing.T) {
			got, err := parseProcArgs2(tt.Raw)
			if tt.WantErr {
				if err == nil {
					t.Fatalf("parseProcArgs2 = %q, want an error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseProcArgs2: %v", err)
			}
			if !slices.Equal(got, tt.Want) {
				t.Errorf("parseProcArgs2 = %q, want %q", got, tt.Want)
			}
		})
	}
}

type procStatTest struct {
	Name    string
	Stat    string
	Want    int
	WantErr bool
}

var procStatTests = []procStatTest{
	{Name: "plain", Stat: "42 (cg) S 7 42 42 0 -1 4194560", Want: 7},
	{Name: "comm with space", Stat: "42 (my prog) S 7 42 42 0", Want: 7},
	{Name: "comm with paren and spaces", Stat: "42 (a) S 9 (b) R 7 42 42 0", Want: 7},
	{Name: "comm ending in paren", Stat: "42 (x)) S 7 42", Want: 7},
	{Name: "too few fields", Stat: "42 (cg) S", WantErr: true},
	{Name: "no fields", Stat: "42 (cg)", WantErr: true},
	{Name: "no comm", Stat: "42 cg S 7", WantErr: true},
	{Name: "non-numeric ppid", Stat: "42 (cg) S x 42", WantErr: true},
	{Name: "empty", Stat: "", WantErr: true},
}

func TestParseProcStatPpid(t *testing.T) {
	for _, tt := range procStatTests {
		t.Run(tt.Name, func(t *testing.T) {
			got, err := parseProcStatPpid([]byte(tt.Stat))
			if tt.WantErr {
				if err == nil {
					t.Fatalf("parseProcStatPpid = %d, want an error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseProcStatPpid: %v", err)
			}
			if got != tt.Want {
				t.Errorf("parseProcStatPpid = %d, want %d", got, tt.Want)
			}
		})
	}
}

type cmdlineTest struct {
	Name    string
	Cmdline string
	Want    []string
}

var cmdlineTests = []cmdlineTest{
	{Name: "arguments", Cmdline: "cg\x00supervise-run\x00/tmp/x\x00", Want: []string{"cg", "supervise-run", "/tmp/x"}},
	{Name: "no trailing NUL", Cmdline: "cg\x00supervise-run", Want: []string{"cg", "supervise-run"}},
	{Name: "empty argument", Cmdline: "cg\x00\x00x\x00", Want: []string{"cg", "", "x"}},
	{Name: "empty", Cmdline: "", Want: nil},
	{Name: "only NUL", Cmdline: "\x00", Want: nil},
}

func TestSplitCmdline(t *testing.T) {
	for _, tt := range cmdlineTests {
		t.Run(tt.Name, func(t *testing.T) {
			if got := splitCmdline([]byte(tt.Cmdline)); !slices.Equal(got, tt.Want) {
				t.Errorf("splitCmdline(%q) = %q, want %q", tt.Cmdline, got, tt.Want)
			}
		})
	}
}
