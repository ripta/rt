package manager

import (
	"fmt"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/ripta/rt/pkg/structfiles/hcl2any"
	"io"
)

func init() {
	RegisterFormat("hcl2", []string{".hcl"}, nil, HCL2Decoder)
}

func HCL2Decoder(r io.Reader) Decoder {
	return &OnceDecoder{
		Decoder: ToDecoder(hcl2converter, r),
	}
}

func hcl2converter(r io.Reader) (any, error) {
	bs, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	hf, diag := hclsyntax.ParseConfig(bs, "stdin.tf", hcl.Pos{Line: 1, Column: 1})
	if diag.HasErrors() {
		return nil, fmt.Errorf("parsing HCL2: %w", diag)
	}

	if hf.Body == nil {
		return nil, nil
	}

	out, err := hcl2any.Convert(hf)
	if err != nil {
		return nil, fmt.Errorf("converting HCL2: %w", err)
	}

	return out, nil
}
