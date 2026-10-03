package rest

// The two files named review2 are a review's, not the change's. This one asks
// the claim the change rests on — that foldedName and encoding/json agree about
// which key names which field — of every single-rune spelling of one declared
// name. A key the decoder binds but the predicate does not name is a write
// through the door; a key the predicate names but the decoder ignores is a
// refusal of a key that would have bound nothing.

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf16"
)

// foldRow is the field the sweep binds into, and foldName the name it asks
// about: a lowercase declared name, the shape every Spec.Immutable entry has.
type foldRow struct {
	Status string `json:"status"`
}

func foldBinds(t *testing.T, key string) bool {
	t.Helper()
	body, err := json.Marshal(map[string]string{key: "bound"})
	if err != nil {
		t.Fatalf("encode the key %q: %v", key, err)
	}
	var got foldRow
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	return got.Status == "bound"
}

// TestTheFoldNamesExactlyTheKeysTheDecoderBinds sweeps every position of
// "status" against every BMP rune and every printable ASCII one, and asks both
// questions of each spelling: does encoding/json bind it into the field, and
// does the door's predicate name it. The two answers must be one answer.
//
// Single-rune substitutions only: the sweep that covers two positions at once
// is 10^10 cases, and the fold is defined per rune, so a difference that needs
// two substitutions would be a difference in length, which the door answers by
// comparing whole strings.
func TestTheFoldNamesExactlyTheKeysTheDecoderBinds(t *testing.T) {
	const declared = "status"
	var runes []rune
	for r := rune(0x20); r <= 0xFFFF; r++ {
		if utf16.IsSurrogate(r) {
			continue
		}
		runes = append(runes, r)
	}
	// The folding runes past the BMP: Deseret and Warang-Citi capitals fold.
	for r := rune(0x10400); r <= 0x1044F; r++ {
		runes = append(runes, r)
	}

	for _, at := range []int{0, 1, 2, 3, 4, 5} {
		for _, r := range runes {
			key := []rune(declared)
			key[at] = r
			sent := string(key)
			binds := foldBinds(t, sent)
			refused := foldedName(map[string]any{sent: "x"}, []string{declared}) == declared
			switch {
			case binds && !refused:
				t.Fatalf("position %d, U+%04X: encoding/json binds %q into Status and the door does not name it", at, r, sent)
			case !binds && refused:
				t.Fatalf("position %d, U+%04X: the door names %q, which encoding/json binds into nothing", at, r, sent)
			}
		}
	}
}

// TestTheFoldAnswersTheWholeKey: folding is a comparison of whole strings, so a
// prefix of a declared name, a longer name that starts with it, and a name with
// a separator in it name nothing. Pinned because a later "starts with" or
// normalising compare would refuse keys the decoder binds nowhere, and because
// an underscore is how a snake_case body names the same column.
func TestTheFoldAnswersTheWholeKey(t *testing.T) {
	for _, sent := range []string{"statu", "statusnote", "status_note", "status.note", " status", "status ", "statuses", "stat"} {
		if got := foldedName(map[string]any{sent: "x"}, []string{"status"}); got != "" {
			t.Errorf("foldedName names %q for the sent key %q, which folds onto nothing", got, sent)
		}
		if foldBinds(t, sent) {
			t.Errorf("encoding/json binds %q into Status, which the case above says it must not", sent)
		}
	}
	if !strings.EqualFold("ſtatus", "status") {
		t.Fatal("strings.EqualFold no longer folds the long s, so the sweep above tested nothing")
	}
}
