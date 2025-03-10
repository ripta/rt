package hcl2any

import (
	"fmt"
	"reflect"

	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/zclconf/go-cty/cty"
)

func Encode(v any) (*hclwrite.File, error) {
	if v == nil {
		return nil, nil
	}

	hf := hclwrite.NewFile()
	if err := encodeInto(hf.Body(), v); err != nil {
		return nil, err
	}

	return hf, nil
}

func encodeInto(b *hclwrite.Body, e any) error {
	if e == nil {
		return nil
	}

	// Dereference pointers
	rv := reflect.ValueOf(e)
	if rv.Kind() == reflect.Ptr {
		e = rv.Elem().Interface()
	}

	switch val := e.(type) {
	case map[string]any:
		for k, v := range val {
			if err := encodeAttribute(b, k, v); err != nil {
				return withKeyTrace(err, k)
			}
		}

	//case []any:
	//for _, v := range val {
	//	if err := encodeInto(v, b.AppendNewBlock("", nil).Body()); err != nil {
	//		return err
	//	}
	//}

	//case string:
	//	b.SetAttributeValue("value", cty.StringVal(val))

	default:
		return fmt.Errorf("unsupported type %T", e)
	}

	return nil
}

func encodeAttribute(b *hclwrite.Body, k string, v any) error {
	if v == nil {
		b.SetAttributeValue(k, cty.NullVal(cty.String))
		return nil
	}

	switch val := v.(type) {
	case map[string]any:
		block := b.AppendNewBlock(k, nil)
		if err := encodeInto(block.Body(), val); err != nil {
			return err
		}

	case []any:
		vals, err := encodeAsValue(val)
		if err != nil {
			return err
		}

		b.SetAttributeValue(k, vals)

	case string:
		b.SetAttributeValue(k, cty.StringVal(val))

	case bool:
		b.SetAttributeValue(k, cty.BoolVal(val))

	case int:
		b.SetAttributeValue(k, cty.NumberIntVal(int64(val)))

	case float64:
		b.SetAttributeValue(k, cty.NumberFloatVal(val))

	default:
		return fmt.Errorf("unsupported type %T", v)
	}

	return nil
}

func encodeAsValue(v any) (cty.Value, error) {
	switch val := v.(type) {
	case string:
		return cty.StringVal(val), nil
	case bool:
		return cty.BoolVal(val), nil
	case int:
		return cty.NumberIntVal(int64(val)), nil
	case float64:
		return cty.NumberFloatVal(val), nil
	case []any:
		vals := []cty.Value{}
		for _, v := range val {
			val, err := encodeAsValue(v)
			if err != nil {
				return cty.Value{}, err
			}

			vals = append(vals, val)
		}

		if cty.CanListVal(vals) {
			return cty.ListVal(vals), nil
		}

		if !allElements(vals, func(v cty.Value) bool { return v.Type().IsObjectType() }) {
			return cty.TupleVal(vals), nil
		}

		return cty.ListVal(vals), nil
	case map[string]any:
		vals := map[string]cty.Value{}
		for k, v := range val {
			val, err := encodeAsValue(v)
			if err != nil {
				return cty.Value{}, err
			}

			vals[k] = val
		}

		return cty.ObjectVal(vals), nil
	default:
		return cty.Value{}, fmt.Errorf("unsupported type %T", v)
	}
}

func allElements(vals []cty.Value, f func(cty.Value) bool) bool {
	for _, v := range vals {
		if !f(v) {
			return false
		}
	}

	return true
}
