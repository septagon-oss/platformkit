package internal

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"
)

type revisionInput int64

func (r *revisionInput) UnmarshalJSON(data []byte) error {
	var value int64
	if len(data) > 0 && data[0] == '"' {
		var text string
		if err := json.Unmarshal(data, &text); err != nil {
			return err
		}
		parsed, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return err
		}
		if strconv.FormatInt(parsed, 10) != text {
			return fmt.Errorf("revision string must be a canonical signed integer")
		}
		value = parsed
	} else {
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		var number json.Number
		if err := decoder.Decode(&number); err != nil {
			return err
		}
		text := number.String()
		mantissa, exponent, hasExponent := strings.Cut(strings.ToLower(text), "e")
		if strings.Trim(mantissa, "-0.") == "" {
			value = 0
		} else {
			if hasExponent {
				power, err := strconv.Atoi(exponent)
				if err != nil || power > len(mantissa)+20 || power < -len(mantissa)-20 {
					return fmt.Errorf("revision must be a signed 64-bit integer")
				}
			}
			rational, ok := new(big.Rat).SetString(text)
			if !ok || !rational.IsInt() || !rational.Num().IsInt64() {
				return fmt.Errorf("revision must be a signed 64-bit integer")
			}
			value = rational.Num().Int64()
		}
	}
	*r = revisionInput(value)
	return nil
}

func (revisionInput) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{AnyOf: []*huma.Schema{
		{Type: huma.TypeString, Format: "int64", Pattern: `^(0|-?[1-9][0-9]*)$`},
		{Type: huma.TypeInteger, Format: "int64"},
	}}
}
