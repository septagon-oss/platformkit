package contracts

import (
	"encoding/json"
	"testing"
)

// The revision a declaration names is a count, so the door takes it as a JSON
// integer: a body that quotes the number, fractions it, words it or names a
// magnitude past int64 is refused rather than guessed at.
func TestLocaleRevisionBindsOnlyAnExactInteger(t *testing.T) {
	for _, spelling := range []string{`0`, `1`, `42`, `-1`, `9223372036854775807`} {
		var held LocaleRevision
		if err := json.Unmarshal([]byte(spelling), &held); err != nil {
			t.Errorf("Unmarshal(%s) = %v, want the counter bound", spelling, err)
		}
	}
	for _, spelling := range []string{`"1"`, `1.5`, `1e3`, `"one"`, `9223372036854775808`} {
		var held LocaleRevision
		if err := json.Unmarshal([]byte(spelling), &held); err == nil {
			t.Errorf("Unmarshal(%s) = %d, want a refusal", spelling, held)
		}
	}
}

// A declaration that names no revision states an intent rather than a change from
// something, and the command reads that absence as nil and not as zero.
func TestADeclarationThatNamesNoRevisionLeavesItAbsent(t *testing.T) {
	var in SetLocale
	if err := json.Unmarshal([]byte(`{"default":"en","supported":["pt-PT"]}`), &in); err != nil {
		t.Fatal(err)
	}
	if in.ExpectedRevision != nil {
		t.Errorf("an unversioned declaration carries revision %d, want none", *in.ExpectedRevision)
	}
}
