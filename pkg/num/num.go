package num

import "math/big"

type Num struct {
	rat  *big.Rat
	real *RRA
}

func (n *Num) String() string {
	if n.real != nil {
		return "real"
	}
	if n.rat != nil {
		return n.rat.String()
	}
	return "0"
}

func FromInt(a int64) *Num {
	return &Num{
		rat: big.NewRat(a, 1),
	}
}

func FromRat(a, b int64) *Num {
	return &Num{
		rat: big.NewRat(a, b),
	}
}

func Zero() *Num {
	return &Num{
		rat: big.NewRat(0, 1),
	}
}
