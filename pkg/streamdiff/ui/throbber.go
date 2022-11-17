package ui

type Throbber struct {
	values []string
	index  int
}

func NewThrobber(values []string) *Throbber {
	return &Throbber{
		values: values,
	}
}

func (t *Throbber) Next() string {
	cur := t.values[t.index]

	t.index++
	if t.index >= len(t.values) {
		t.index = 0
	}

	return cur
}

type ThrobberSet struct {
	values []string
	set    map[string]*Throbber
}

func NewThrobberSet(values []string) *ThrobberSet {
	return &ThrobberSet{
		values: values,
		set:    map[string]*Throbber{},
	}
}

func (ts *ThrobberSet) Next(key string) string {
	t, ok := ts.set[key]
	if !ok {
		t = NewThrobber(ts.values)
		ts.set[key] = t
	}

	return t.Next()
}
