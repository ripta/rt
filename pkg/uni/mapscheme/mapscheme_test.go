package mapscheme

import "testing"

func TestRegistry(t *testing.T) {
	t.Parallel()

	preNames := Names()

	name := "__test__"
	fn := MustGenerateFromString("ABC", "abc")
	MustRegister(name, fn)

	postNames := Names()
	if len(preNames)+1 != len(postNames) {
		t.Errorf("expected postNames (%d) to be exactly 1 larger than preNames (%d)", len(postNames), len(preNames))
		return
	}

	if !Has(name) {
		t.Errorf("expected function %q to be registered, but it isn't", name)
		return
	}

	actualFn := Get(name)
	if actualFn == nil {
		t.Error("expected existing function to be returned")
		return
	}
}
