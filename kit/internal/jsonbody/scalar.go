package jsonbody

import (
	"encoding/json"
	"fmt"
	"math/big"
	"regexp"
	"strconv"
	"strings"
)

var decimalDigits = regexp.MustCompile(`^-?(0|[1-9][0-9]*)$`)

// Decimal returns the canonical signed int64 spelling of one JSON scalar.
// Exponents are bounded by the mantissa length before any big-number allocation.
func Decimal(raw []byte) (string, error) {
	s := strings.TrimSpace(string(raw))
	invalid := fmt.Errorf("expected an exact integer in -9223372036854775808 through 9223372036854775807")
	if !json.Valid(raw) {
		return "", invalid
	}
	if strings.HasPrefix(s, `"`) {
		if err := json.Unmarshal(raw, &s); err != nil {
			return "", err
		}
		if !decimalDigits.MatchString(s) {
			return "", invalid
		}
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return "", invalid
		}
		return strconv.FormatInt(n, 10), nil
	}
	mantissa, exponent, hasExponent := strings.Cut(strings.ToLower(s), "e")
	if strings.Trim(mantissa, "-0.") == "" {
		return "0", nil
	}
	if hasExponent {
		power, err := strconv.Atoi(exponent)
		if err != nil || power > len(mantissa)+20 || power < -len(mantissa)-20 {
			return "", invalid
		}
	}
	n, ok := new(big.Rat).SetString(s)
	if !ok || !n.IsInt() || !n.Num().IsInt64() {
		return "", invalid
	}
	return n.Num().String(), nil
}
