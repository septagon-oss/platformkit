package legible_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/ui/legible"
)

// A label a person reads before a field is copy somebody wrote in Go, whatever
// punctuation follows it. It is not a key, an id, a number or a value a person
// typed, so the number may not decline to count it.
func TestALabelEndingInAColonIsCopy(t *testing.T) {
	for _, label := range []string{"Name:", "Email:", "Status:", "Total:", "Note:", "To:"} {
		if legible.Exempt(label) {
			t.Errorf("Exempt(%q) = true: a field's label is copy, not data", label)
		}
	}
}

// The same rule seen from the report a page gets: a hard-coded label on a served
// document is named as TEXT with its path, and not declined as DATA.
func TestAHardCodedLabelIsNamedOnThePageThatHoldsIt(t *testing.T) {
	body := []byte(`<!doctype html><html><head><title>⟦x⟧</title></head>` +
		`<body><form><label>Name:</label><input name="n"></form></body></html>`)
	collected, err := legible.Scan(body, func(text string) bool { return strings.HasPrefix(text, "⟦") })
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if !slices.ContainsFunc(collected, func(s legible.String) bool { return s.Text == "Name:" }) {
		t.Fatalf("the scan did not read the label at all: %+v", collected)
	}
	unreachable, _ := legible.Report("GET /form", collected)
	// The same label without its colon is the control: it is named today, so the line
	// below is spelled the way the report spells every literal.
	control, err := legible.Scan([]byte(strings.Replace(string(body), "Name:", "Name", 1)), nil)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if named, _ := legible.Report("GET /form", control); !slices.Contains(named,
		`TEXT GET /form html[1] > body[2] > form[1] > label[1]#text "Name"`) {
		t.Fatalf("the control label is not named at the expected path: %v", named)
	}
	want := `TEXT GET /form html[1] > body[2] > form[1] > label[1]#text "Name:"`
	if !slices.Contains(unreachable, want) {
		t.Errorf("the report does not name the hard-coded label as\n%s\ngot:\n%s", want, strings.Join(unreachable, "\n"))
	}
}

// Weekday and month names are words a translator is asked for. The rule's own
// comment says dates are exempt "in the numeric forms this kernel renders, and only
// those"; an English day and month in front of a time is not a numeric form.
func TestAnEnglishDayAndMonthAreCopy(t *testing.T) {
	for _, text := range []string{"Sat 1 Jan 09:00 UTC", "Due 12 Mon 09:00 UTC"} {
		if legible.Exempt(text) {
			t.Errorf("Exempt(%q) = true: the words in it are English copy, not data", text)
		}
	}
}
