package jsonbody

import (
	"fmt"
	"math/big"
	"reflect"
	"strings"
)

// checkInteger applies exact numeric declarations to a covered scalar; other types keep
// their existing validator. Nullability is decided by the binding/schema owner.
func checkInteger(digits string, tag reflect.StructTag) error {
	n, _ := new(big.Rat).SetString(digits)
	for _, key := range []string{"minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum", "multipleOf"} {
		if literal := tag.Get(key); literal != "" {
			bound, ok := new(big.Rat).SetString(literal)
			if !ok {
				return fmt.Errorf("invalid declared %s %q", key, literal)
			}
			if !within(n, bound, key) {
				return fmt.Errorf("integer must satisfy %s %s", key, literal)
			}
		}
	}
	if enum := tag.Get("enum"); enum != "" {
		for literal := range strings.SplitSeq(enum, ",") {
			allowed, err := Decimal([]byte(strings.TrimSpace(literal)))
			if err == nil && digits == allowed {
				return nil
			}
		}
		return fmt.Errorf("integer is outside the declared enum")
	}
	return nil
}

func within(n, bound *big.Rat, key string) bool {
	switch key {
	case "minimum":
		return n.Cmp(bound) >= 0
	case "maximum":
		return n.Cmp(bound) <= 0
	case "exclusiveMinimum":
		return n.Cmp(bound) > 0
	case "exclusiveMaximum":
		return n.Cmp(bound) < 0
	case "multipleOf":
		return bound.Sign() > 0 && new(big.Rat).Quo(n, bound).IsInt()
	}
	return true
}

// Audit refuses malformed defaults and scalar restrictions during composition.
func Audit(s *Shape) error { return audit(s, map[*Shape]bool{}) }

func audit(s *Shape, seen map[*Shape]bool) error {
	if s == nil || seen[s] {
		return nil
	}
	seen[s] = true
	if err := audit(s.Elem, seen); err != nil {
		return err
	}
	for _, f := range s.Fields {
		if err := audit(f.Shape, seen); err != nil {
			return err
		}
		if raw := f.Tag.Get("default"); raw != "" && hasInteger(f.Shape, map[*Shape]bool{}) {
			if err := Decode([]byte(raw), reflect.New(f.Shape.Type).Interface()); err != nil {
				return fmt.Errorf("%s default: %w", f.Name, err)
			}
		}
		inner := f.Shape.base()
		if inner == nil || !inner.Integer {
			continue
		}
		if err := auditRestrictions(f.Tag); err != nil {
			return fmt.Errorf("%s: %w", f.Name, err)
		}
		for _, key := range []string{"default", "example"} {
			if raw := f.Tag.Get(key); raw != "" {
				digits, err := Decimal([]byte(raw))
				if err == nil {
					err = checkInteger(digits, f.Tag)
				}
				if err != nil {
					return fmt.Errorf("%s %s: %w", f.Name, key, err)
				}
			}
		}
	}
	return nil
}

func auditRestrictions(tag reflect.StructTag) error {
	lo, hi := new(big.Rat).SetInt64(-9223372036854775808), new(big.Rat).SetInt64(9223372036854775807)
	for _, key := range []string{"minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum", "multipleOf"} {
		literal := tag.Get(key)
		if literal == "" {
			continue
		}
		bound, ok := new(big.Rat).SetString(literal)
		if !ok || (key == "multipleOf" && bound.Sign() <= 0) {
			return fmt.Errorf("invalid declared %s %q", key, literal)
		}
		switch key {
		case "minimum", "exclusiveMinimum":
			bound = integralBound(bound, true, key == "exclusiveMinimum")
			if bound.Cmp(lo) > 0 {
				lo = bound
			}
		case "maximum", "exclusiveMaximum":
			bound = integralBound(bound, false, key == "exclusiveMaximum")
			if bound.Cmp(hi) < 0 {
				hi = bound
			}
		}
	}
	if lo.Cmp(hi) > 0 {
		return fmt.Errorf("contradictory signed 64-bit bounds")
	}
	if enum := tag.Get("enum"); enum != "" {
		for literal := range strings.SplitSeq(enum, ",") {
			if _, err := Decimal([]byte(strings.TrimSpace(literal))); err != nil {
				return fmt.Errorf("invalid enum: %w", err)
			}
		}
	}
	return nil
}

func integralBound(bound *big.Rat, lower, exclusive bool) *big.Rat {
	whole, remainder := new(big.Int), new(big.Int)
	whole.QuoRem(bound.Num(), bound.Denom(), remainder)
	if lower && (remainder.Sign() > 0 || (exclusive && remainder.Sign() == 0)) {
		whole.Add(whole, big.NewInt(1))
	}
	if !lower && (remainder.Sign() < 0 || (exclusive && remainder.Sign() == 0)) {
		whole.Sub(whole, big.NewInt(1))
	}
	return new(big.Rat).SetInt(whole)
}
