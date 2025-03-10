// Package hcl2any is a version of github.com/hashicorp/hcl/cmd/hcldec that
// doesn't require a schema to decode HCL2 files. It does not yet support
// every data type that HCL2 supports.
package hcl2any

import (
	"fmt"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/convert"
)

// Convert converts an HCL file to a map[string]any.
func Convert(hf *hcl.File) (map[string]any, error) {
	b, ok := hf.Body.(*hclsyntax.Body)
	if !ok {
		return nil, fmt.Errorf("expected hclsyntax.Body, got %T", hf.Body)
	}

	return ConvertBody(b)
}

// ConvertBody converts an HCL body to a map[string]any.
func ConvertBody(b *hclsyntax.Body) (map[string]any, error) {
	out := map[string]any{}
	if err := DecodeBody(b, out); err != nil {
		return nil, err
	}

	return out, nil
}

// DecodeBody decodes an HCL body into a preällocated map[string]any.
func DecodeBody(b *hclsyntax.Body, out map[string]any) error {
	for _, block := range b.Blocks {
		if err := DecodeBlock(block, out); err != nil {
			return withKeyTrace(err, block.Type)
		}
	}

	for k, v := range b.Attributes {
		expr, err := ConvertExpression(v.Expr)
		if err != nil {
			return withKeyTrace(err, k)
		}

		out[k] = expr
	}

	return nil
}

// DecodeBlock decodes an HCL block into a preällocated map[string]any.
func DecodeBlock(b *hclsyntax.Block, out map[string]any) error {
	key := b.Type
	trace := []string{key}
	for _, label := range b.Labels {
		if _, ok := out[key]; ok {
			val, ok := out[key].(map[string]any)
			if !ok {
				return withKeyTrace(fmt.Errorf("expected map[string]any, got %T", out[key]), trace...)
			}

			out = val
		} else {
			out[key] = map[string]any{}
			out = out[key].(map[string]any)
		}

		key = label
		trace = append(trace, key)
	}

	val, err := ConvertBody(b.Body)
	if err != nil {
		return err
	}

	if curr, ok := out[key]; ok {
		switch typ := curr.(type) {
		case []any:
			out[key] = append(typ, val)
		default:
			return fmt.Errorf("expected []any at %q, got %T", key, curr)
		}
	} else {
		out[key] = []any{val}
	}

	return nil
}

// ConvertExpression converts an HCL expression to a Go value.
func ConvertExpression(e hclsyntax.Expression) (any, error) {
	switch v := e.(type) {
	case *hclsyntax.LiteralValueExpr:
		return literalValue(v)
	case *hclsyntax.TemplateExpr:
		return convertTemplateExpr(v)
	case *hclsyntax.TupleConsExpr:
		vals := []any{}
		for _, elem := range v.Exprs {
			val, err := ConvertExpression(elem)
			if err != nil {
				return nil, err
			}

			vals = append(vals, val)
		}

		return vals, nil
	default:
		return nil, fmt.Errorf("unsupported expression type %T in expression", e)
	}
}

// convertTemplateExpr converts an HCL template expression to a string.
func convertTemplateExpr(e *hclsyntax.TemplateExpr) (string, error) {
	if e.IsStringLiteral() {
		v, err := e.Value(nil)
		if err != nil {
			return "", err
		}
		return escape(v), nil
	}

	sb := strings.Builder{}
	for _, part := range e.Parts {
		s, err := convertStringPart(part)
		if err != nil {
			return "", err
		}

		sb.WriteString(s)
	}

	return sb.String(), nil
}

func escape(v cty.Value) string {
	return strings.Replace(v.AsString(), "${", "$${", -1)
}

func convertStringPart(e hclsyntax.Expression) (string, error) {
	switch v := e.(type) {
	case *hclsyntax.LiteralValueExpr:
		if v.Val.IsNull() {
			return "null", nil
		}

		s, err := convert.Convert(v.Val, cty.String)
		if err != nil {
			return "", err
		}

		return escape(s), nil
	default:
		return "", fmt.Errorf("unsupported expression type %T in string part", e)
	}
}

func literalValue(e *hclsyntax.LiteralValueExpr) (any, error) {
	v := e.Val
	t := v.Type()

	if v.IsMarked() {
		return nil, fmt.Errorf("value is unserializeable, because it has marks")
	}
	if !v.IsKnown() {
		return nil, fmt.Errorf("value is unserializeable, because it is not known")
	}

	if v.IsNull() {
		return nil, nil
	}

	if t.IsPrimitiveType() {
		switch t {
		case cty.String:
			return convert.Convert(v, cty.String)
		case cty.Number:
			f, _ := v.AsBigFloat().Float64()
			return f, nil
		case cty.Bool:
			return v.True(), nil
		default:
			return nil, fmt.Errorf("unsupported primitive type %s", t.FriendlyName())
		}
	}

	return nil, fmt.Errorf("unsupported literal type %s", t.FriendlyName())
}
