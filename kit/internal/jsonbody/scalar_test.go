package jsonbody

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestIntegerNumberSpellingsNeverPassThroughFloat(t *testing.T) {
	accepted := map[string]string{
		"0e" + strings.Repeat("9", 100000): "0",
		`0`:                                "0", `-0`: "0", `"-0"`: "0", `-1`: "-1", `1.0`: "1", `1e3`: "1000",
		`9007199254740991`: "9007199254740991", `9007199254740992`: "9007199254740992",
		`9007199254740993`: "9007199254740993", `9007199254740993.0`: "9007199254740993",
		`90071992547409930e-1`: "9007199254740993", `-9007199254740993`: "-9007199254740993",
		`9223372036854775807`: "9223372036854775807", `-9223372036854775808`: "-9223372036854775808",
		`"900719925474099\u0033"`: "9007199254740993", `0e999999999`: "0", `0e-999999999`: "0",
	}
	for raw, want := range accepted {
		got, err := Decimal([]byte(raw))
		if err != nil || got != want {
			t.Errorf("%s = %q, %v; want %q", raw, got, err, want)
		}
	}
	for _, raw := range []string{`1.5`, `1e-1`, `9007199254740993.1`, `1e999999999`, `1e-999999999`,
		`9223372036854775808`, `-9223372036854775809`, `"9223372036854775808"`, `"-9223372036854775809"`,
		`"01"`, `"+1"`, `"1.0"`, `"1e3"`, `" 1"`, `""`, `"١"`, `"NaN"`, `"Infinity"`, `true`, `[]`, `{}`, `null`} {
		if got, err := Decimal([]byte(raw)); err == nil {
			t.Errorf("accepted %s as %s", raw, got)
		}
	}
}

type integerAlias int64
type integerPrivate int64

func (integerPrivate) MarshalJSON() ([]byte, error) { return []byte(`"private"`), nil }
func (*integerPrivate) UnmarshalJSON([]byte) error  { return nil }

type integerEmbedded struct {
	Promoted int64 `json:"promoted"`
}
type integerShapes struct {
	integerEmbedded
	Amount  integerAlias     `json:"amount"`
	Small   int32            `json:"small"`
	Native  int              `json:"native"`
	Pointer *int64           `json:"pointer"`
	Slice   []int64          `json:"slice"`
	Array   [1]int64         `json:"array"`
	Map     map[string]int64 `json:"map"`
	Dynamic any              `json:"dynamic"`
	Quoted  int64            `json:"quoted,string"`
	Hidden  int64            `json:"-"`
	Omitted int64            `json:"omitted,omitzero"`
	Private integerPrivate   `json:"private"`
	Time    time.Time        `json:"time"`
	Raw     json.RawMessage  `json:"raw"`
}

func TestOnlyDeclaredInt64ValuesChangeRepresentation(t *testing.T) {
	const n int64 = 9007199254740993
	value := integerShapes{integerEmbedded: integerEmbedded{n}, Amount: integerAlias(n), Small: 11, Native: 7,
		Pointer: new(n), Slice: []int64{n}, Array: [1]int64{n}, Map: map[string]int64{"value": n},
		Dynamic: n, Quoted: n, Raw: json.RawMessage(`{"value":9007199254740993}`)}
	before, _ := json.Marshal(value)
	var encoded bytes.Buffer
	if err := Encode(&encoded, value); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := Tokens(encoded.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]any{
		"amount": "9007199254740993", "pointer": "9007199254740993", "promoted": "9007199254740993",
		"dynamic": "9007199254740993", "quoted": "9007199254740993", "private": "private",
		"small": json.Number("11"), "native": json.Number("7"),
		"slice": []any{"9007199254740993"}, "array": []any{"9007199254740993"},
		"map": map[string]any{"value": "9007199254740993"},
		"raw": map[string]any{"value": json.Number("9007199254740993")},
	} {
		if !reflect.DeepEqual(got[key], want) {
			t.Errorf("%s = %v, want %v", key, got[key], want)
		}
	}
	if _, present := got["omitted"]; present {
		t.Fatal("zero should be omitted")
	}
	after, _ := json.Marshal(value)
	if !bytes.Equal(before, after) {
		t.Fatal("encoding mutated the input")
	}
	var decoded integerShapes
	if err := Decode(encoded.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Amount != integerAlias(n) || decoded.Quoted != n || *decoded.Pointer != n {
		t.Fatal(decoded)
	}
}

func TestExactBindingKeepsDuplicateAndEmbeddedFieldSemantics(t *testing.T) {
	type nested struct {
		A int64 `json:"a"`
		B int64 `json:"b"`
	}
	type body struct {
		Value nested `json:"value"`
		Count int32  `json:"count"`
	}
	var got body
	if err := Decode([]byte(`{"value":{"a":"9007199254740993"},"VALUE":{"b":2}}`), &got); err != nil {
		t.Fatal(err)
	}
	if got.Value.A != 9007199254740993 || got.Value.B != 2 {
		t.Fatal(got)
	}
	for _, raw := range []string{`{"value":{"a":"01"}}`, `{"count":"1"}`, `{"value":{}}{}`, `{"value":{"a":01}}`} {
		if err := Decode([]byte(raw), &got); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	var untyped map[string]any
	if err := Decode([]byte(`{"n":9007199254740993}`), &untyped); err != nil || untyped["n"] != json.Number("9007199254740993") {
		t.Fatalf("%v, %v", untyped, err)
	}
}

type integerMapKey struct{ Name string }

func (k integerMapKey) MarshalText() ([]byte, error) { return []byte(k.Name), nil }

func TestIntegerInterfacesUnderMapKeysKeepTheirDigits(t *testing.T) {
	for _, value := range []any{
		map[int]any{7: int64(9007199254740993)},
		map[uint64]any{7: int64(9007199254740993)},
		map[integerMapKey]any{{"7"}: int64(9007199254740993)},
	} {
		var encoded bytes.Buffer
		if err := Encode(&encoded, value); err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(encoded.String()) != `{"7":"9007199254740993"}` {
			t.Fatal(encoded.String())
		}
	}
}

func TestDeclaredIntegerRestrictionsUseExactSourceTags(t *testing.T) {
	type bounded struct {
		Value int64 `json:"value" minimum:"9007199254740993" maximum:"9007199254740995" multipleOf:"1"`
	}
	for _, raw := range []string{`9007199254740993`, `"9007199254740993"`, `90071992547409930e-1`} {
		var value bounded
		if err := Decode([]byte(`{"value":`+raw+`}`), &value); err != nil {
			t.Fatal(err)
		}
	}
	for _, raw := range []string{`9007199254740992`, `"9007199254740992"`, `9007199254740996`, `"9007199254740996"`} {
		var value bounded
		if err := Decode([]byte(`{"value":`+raw+`}`), &value); err == nil {
			t.Fatal("accepted " + raw)
		}
	}
	type invalid struct {
		Value int64 `json:"value" default:"9223372036854775808"`
	}
	if err := Audit(Describe(reflect.TypeFor[invalid]())); err == nil {
		t.Fatal("invalid default accepted")
	}
}
