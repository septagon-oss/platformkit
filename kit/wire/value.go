package wire

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// A number is spelled exactly within this window: 512 significant digits and an exponent
// within ten thousand. Outside it a literal keeps its own spelling behind a `=` no
// canonical text starts with, so two spellings of one huge value can refuse while no
// change between two documents can hide behind the window.
const (
	numberDigits   = 512
	numberExponent = 10_000

	// numberPlain is the largest exponent written out in digits rather than in scientific
	// notation. Beyond it a value is its normalised scientific text either way.
	numberPlain = 32
)

// wireNumberText is a JSON number as one canonical text: equal values spell the same text,
// different values never spell one another, and nothing travels through a float64 on the
// way. A literal the grammar refuses, or one outside the exact window, keeps its own
// spelling behind `=`.
func wireNumberText(raw json.Number) string {
	literal := string(raw)
	negative, digits, exponent, ok := splitNumber(literal)
	if !ok {
		return "=" + literal
	}
	if digits == "0" {
		return "0"
	}
	if negative {
		return "-" + renderNumber(digits, exponent)
	}
	return renderNumber(digits, exponent)
}

// splitNumber reads a JSON number literal into its sign, its significant digits with
// leading and trailing zeros removed, and the power of ten its last digit sits at, so the
// value is digits × 10^exponent. That triple is a normal form: one value, one triple. A
// literal the JSON grammar refuses, or whose triple falls outside the exact window, is not
// ok.
func splitNumber(literal string) (negative bool, digits string, exponent int, ok bool) {
	text, negative := literal, false
	if rest, found := strings.CutPrefix(text, "-"); found {
		text, negative = rest, true
	}
	mantissa, power, hasPower := strings.Cut(text, "e")
	if !hasPower {
		mantissa, power, hasPower = strings.Cut(text, "E")
	}
	integer, fraction, hasFraction := strings.Cut(mantissa, ".")
	if !wireNumberInteger(integer) || (hasFraction && !wireDigits(fraction)) {
		return false, "", 0, false
	}
	shift := 0
	if hasPower {
		parsed, err := strconv.Atoi(power)
		if err != nil {
			return false, "", 0, false
		}
		shift = parsed
	}
	digits = strings.TrimLeft(integer+fraction, "0")
	if digits == "" {
		return false, "0", 0, true
	}
	// Stripping trailing zeros below raises the exponent by at most one per digit, so this
	// bound asks that no reading of the literal can land in the window before it is spent,
	// and that the raising itself cannot overflow.
	exponent = shift - len(fraction)
	if exponent > numberExponent || exponent < -(numberExponent+len(digits)+1) {
		return false, "", 0, false
	}
	for strings.HasSuffix(digits, "0") {
		digits, exponent = digits[:len(digits)-1], exponent+1
	}
	if exponent < -numberExponent || exponent > numberExponent || len(digits) > numberDigits {
		return false, "", 0, false
	}
	return negative, digits, exponent, true
}

// renderNumber writes digits × 10^exponent as decimal: plain digits while the exponent
// fits a line, normalised scientific notation beyond it. The digits carry no leading or
// trailing zero, so one value has one rendering.
func renderNumber(digits string, exponent int) string {
	switch {
	case exponent < -numberPlain || exponent > numberPlain:
		fraction := ""
		if len(digits) > 1 {
			fraction = "." + digits[1:]
		}
		return digits[:1] + fraction + "e" + strconv.Itoa(exponent)
	case exponent > 0:
		return digits + strings.Repeat("0", exponent)
	case exponent == 0:
		return digits
	case len(digits) > -exponent:
		return digits[:len(digits)+exponent] + "." + digits[len(digits)+exponent:]
	default:
		return "0." + strings.Repeat("0", -exponent-len(digits)) + digits
	}
}

// wireNumberInteger is the integer half of a JSON number: digits with no leading zero, or
// the single 0 the grammar allows where 01 would refuse.
func wireNumberInteger(text string) bool {
	return wireDigits(text) && (text == "0" || text[0] != '0')
}

// wireDigits is a non-empty run of decimal digits.
func wireDigits(text string) bool {
	if text == "" {
		return false
	}
	for i := 0; i < len(text); i++ {
		if text[i] < '0' || text[i] > '9' {
			return false
		}
	}
	return true
}

// wireValueToken is a string as one segment token: written as itself while it reads back
// as exactly one member of its own kind, quoted when it cannot. A token is quoted when it
// is empty, holds a space, a tab, a newline, a quote, a backslash or the `=` marker, opens
// a JSON object or array, or would read back as a number, a bool or a null rather than as
// the string it is.
func wireValueToken(text string) string {
	if text == "" || text[0] == '{' || text[0] == '[' || strings.ContainsAny(text, " \t\n\"|\\=") || wireReadsAsJSON(text) {
		return strconv.Quote(text)
	}
	return text
}

// wireReadsAsJSON is whether text is the spelling of a JSON value that is not a string,
// which is why such a string cannot be spelled as itself.
func wireReadsAsJSON(text string) bool {
	if text == "true" || text == "false" || text == "null" {
		return true
	}
	_, _, _, ok := splitNumber(text)
	return ok
}

// wireMemberToken is one schema value — an enum member, or the value of a bound the
// signature spells — as one segment token: a string spelled so it reads back as a string,
// a number at its exact value, and anything else as the JSON it is.
func wireMemberToken(value any) string {
	switch typed := value.(type) {
	case string:
		return wireValueToken(typed)
	case json.Number:
		return wireNumberText(typed)
	default:
		return wireJSONToken(value)
	}
}

// tokenSeparators escapes the two characters a token cannot hold inside JSON text. Compact
// JSON puts a space or a pipe only inside a string of its own, so the escape stays within
// that string and reads back as the same value.
var tokenSeparators = strings.NewReplacer(" ", `\u0020`, "|", `\u007c`)

// wireJSONToken is the compact JSON of a value that is neither a string nor a number: a
// bool, a null, an object or an array. A number it carries keeps the literal the document
// wrote, so an equivalent re-spelling inside one member can refuse; nothing that changed
// can hide.
func wireJSONToken(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return strconv.Quote(fmt.Sprint(value))
	}
	return tokenSeparators.Replace(string(encoded))
}

// wireEnumText spells a set of enum members as one segment: every member as a token,
// deduplicated and sorted as tokens, joined with `|`. A set is a set, so ["a","a"] and
// ["a"] are one contract and one spelling, and no two distinct sets share a spelling.
func wireEnumText(members []any) string {
	tokens := make([]string, len(members))
	for i, member := range members {
		tokens[i] = wireMemberToken(member)
	}
	slices.Sort(tokens)
	return strings.Join(slices.Compact(tokens), "|")
}
