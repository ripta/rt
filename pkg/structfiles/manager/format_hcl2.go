package manager

import (
	"fmt"
	"io"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"

	"github.com/ripta/rt/pkg/structfiles/hcl2any"
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

func HCL2Encoder(w io.Writer) (Encoder, Closer) {
	return EncoderFunc(func(v any) error {
		hf, err := hcl2any.Encode(v)
		if err != nil {
			return fmt.Errorf("encoding HCL2: %w", err)
		}

		if _, err := w.Write(hf.Bytes()); err != nil {
			return fmt.Errorf("writing HCL2: %w", err)
		}

		return nil
	}), noCloser
}
