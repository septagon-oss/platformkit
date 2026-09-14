package examples

import (
	"fmt"
	"go/format"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

// GoProps returns a copyable Go declaration of the captured properties. Rich
// slots remain trusted application composition and are documented separately.
// Unsupported callback or opaque values are refused, never printed as addresses.
func (e Example) GoProps() (string, error) {
	if !e.props.IsValid() {
		return "", fmt.Errorf("example has no typed properties")
	}
	if err := e.validateCapture(); err != nil {
		return "", err
	}
	literal, err := goLiteral(e.props)
	if err != nil {
		return "", err
	}
	formatted, err := format.Source([]byte("props := " + literal))
	return string(formatted), err
}

func goLiteral(v reflect.Value) (string, error) {
	if !v.IsValid() {
		return "nil", nil
	}
	switch v.Kind() {
	case reflect.Interface:
		if v.IsNil() {
			return "nil", nil
		}
		return goLiteral(v.Elem())
	case reflect.Pointer:
		if v.IsNil() {
			return "nil", nil
		}
		value, err := goLiteral(v.Elem())
		return "new(" + value + ")", err
	case reflect.String:
		return strconv.Quote(v.String()), nil
	case reflect.Bool:
		return strconv.FormatBool(v.Bool()), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(v.Int(), 10), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(v.Uint(), 10), nil
	case reflect.Float32, reflect.Float64:
		return strconv.FormatFloat(v.Float(), 'g', -1, v.Type().Bits()), nil
	case reflect.Struct, reflect.Slice, reflect.Array, reflect.Map:
		var entries []string
		appendEntry := func(key string, value reflect.Value) error {
			text, err := goLiteral(value)
			if err == nil {
				entries = append(entries, key+text+",")
			}
			return err
		}
		switch v.Kind() {
		case reflect.Struct:
			for field := range v.Type().Fields() {
				value := v.FieldByIndex(field.Index)
				if !field.IsExported() || value.IsZero() {
					continue
				}
				if err := appendEntry(field.Name+": ", value); err != nil {
					return "", err
				}
			}
		case reflect.Map:
			if v.Type().Key().Kind() != reflect.String {
				return "", fmt.Errorf("unsupported Go map key")
			}
			keys := v.MapKeys()
			slices.SortFunc(keys, func(a, b reflect.Value) int { return strings.Compare(a.String(), b.String()) })
			for _, key := range keys {
				if err := appendEntry(strconv.Quote(key.String())+": ", v.MapIndex(key)); err != nil {
					return "", err
				}
			}
		default:
			for i := range v.Len() {
				if err := appendEntry("", v.Index(i)); err != nil {
					return "", err
				}
			}
		}
		return v.Type().String() + "{\n" + strings.Join(entries, "\n") + "\n}", nil
	default:
		return "", fmt.Errorf("%s requires trusted Go composition", v.Type())
	}
}
