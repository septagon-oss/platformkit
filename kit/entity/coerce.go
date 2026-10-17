package entity

import (
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
)

// Coerce reads a list filter or scalar default using the field's scalar type.
// Strings remain literal; malformed typed values return a named error.
func Coerce(f Field, raw string) (any, error) {
	var (
		v   any
		err error
	)
	switch f.Type {
	case TypeInt:
		v, err = strconv.ParseInt(raw, 10, 64)
	case TypeFloat:
		v, err = strconv.ParseFloat(raw, 64)
	case TypeBool:
		v, err = strconv.ParseBool(raw)
	case TypeUUID:
		v, err = uuid.Parse(raw)
	case TypeTime:
		v, err = time.Parse(time.RFC3339, raw)
	default:
		v = raw
	}
	if err != nil {
		return nil, fmt.Errorf("%s is a %s and %q is not one", f.Name, f.Type, raw)
	}
	return v, nil
}
