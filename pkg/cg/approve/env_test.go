package approve

import (
	"reflect"
	"testing"
)

type disallowedEnvsTest struct {
	name   string
	permit []string
	env    map[string]string
	want   []string
}

var disallowedEnvsTests = []disallowedEnvsTest{
	{name: "no env", env: nil, want: nil},
	{name: "only safe vars", env: map[string]string{"FOO": "1", "HOME": "/x"}, want: nil},
	{name: "dangerous var refused", env: map[string]string{"LD_PRELOAD": "evil.so"}, want: []string{"LD_PRELOAD"}},
	{
		name:   "dangerous var permitted",
		permit: []string{"PATH"},
		env:    map[string]string{"PATH": "/custom"},
		want:   nil,
	},
	{
		name:   "mixed permitted and refused",
		permit: []string{"PATH"},
		env:    map[string]string{"PATH": "/custom", "LD_PRELOAD": "evil.so", "FOO": "1"},
		want:   []string{"LD_PRELOAD"},
	},
	{
		name: "multiple refused sorted",
		env:  map[string]string{"PYTHONPATH": "/x", "DYLD_INSERT_LIBRARIES": "y", "NODE_OPTIONS": "z"},
		want: []string{"DYLD_INSERT_LIBRARIES", "NODE_OPTIONS", "PYTHONPATH"},
	},
}

func TestRuleDisallowedEnvs(t *testing.T) {
	t.Parallel()

	for _, tt := range disallowedEnvsTests {
		t.Run(tt.name, func(t *testing.T) {
			r := Rule{Prefix: []string{"make"}, kind: KindPrefix, PermitUnsafeEnvs: tt.permit}
			if got := r.DisallowedEnvs(tt.env); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("DisallowedEnvs(%v) = %v, want %v", tt.env, got, tt.want)
			}
		})
	}
}
