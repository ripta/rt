package calc

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/ripta/reals/pkg/constructive"
	"github.com/ripta/reals/pkg/unified"
)

const (
	displayPrecisionBits  = -100
	displayDecimalDigits  = 24
	precisionFailureLabel = "<precision error>"
)

func formatReal(r *unified.Real) string {
	if r == nil {
		return "<nil>"
	}

	rat, err := approximateReal(r, displayPrecisionBits)
	if err != nil {
		return fmt.Sprintf("%s: %v", precisionFailureLabel, err)
	}

	s := rat.FloatString(displayDecimalDigits)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
		if s == "" || s == "-" {
			s += "0"
		}
	}
	return s
}

func approximateReal(r *unified.Real, precision int) (*big.Rat, error) {
	if r == nil {
		return nil, fmt.Errorf("nil real")
	}
	if precision >= 0 {
		return nil, fmt.Errorf("precision must be negative (got %d)", precision)
	}
	if !constructive.IsPrecisionValid(precision) {
		return nil, fmt.Errorf("precision %d is out of range", precision)
	}

	approx := constructive.Approximate(r.Constructive(), precision)
	if approx == nil {
		return nil, fmt.Errorf("approximation failed at precision %d", precision)
	}

	exp := int64(-precision)
	denom := new(big.Int).Exp(big.NewInt(2), big.NewInt(exp), nil)
	return new(big.Rat).SetFrac(approx, denom), nil
}
