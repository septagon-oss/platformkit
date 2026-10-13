package legible

import (
	"strings"
	"testing"
)

// TestTheExemptionRule reads §"the exemption rule" as a table. The second half is
// the one that makes the number mean something: every string there is copy somebody
// wrote in Go, and the rule must refuse to look away from it — including from the
// Portuguese sentence, because exempting text that is not English would be the
// flattering version of this rule. The two dates in that half are the shape this rule
// argues with itself: a kernel that renders an instant as "2026-10-04 11:50" exempts
// it, and one that renders it as "Mon 04 Oct …" is a page whose weekday and month
// names nobody translated, which is what the first half must not quietly agree to.
func TestTheExemptionRule(t *testing.T) {
	for _, table := range []struct {
		what  string
		texts []string
		want  bool
	}{
		{"data the number does not count", []string{
			"42", "1.234.567,89", "R$ 1.234,50", "12 %", "4 MB", "2026-10-04",
			"2026-10-04T11:50:56Z",
			"2026-10-04 11:50", "2026-10-04T11:50:56+02:00",
			"2f1e9a70-9e0b-7c31-8a2d-4c5d6e7f8a90", "a1b2c3d4e5f6a7b8c9d0",
			"task:read", "screens.new", "AUTH_DENIED", "text/plain", "pt_BR", "pt-PT",
			"en-XA", "root@acme.localhost", "acme.localhost", "/app/task/tasks",
			"report.pdf", "—", "·", "", "1 234,5", "1.234,50 EUR", "0",
		}, true},
		{"copy the number counts", []string{
			"Hard-coded", "Sign-in", "Follow-up", "Dashboard", "New task", "No results",
			"Ask for access", "Back to the workspace", "Learn more", "Save",
			"onboarding-checklist", "Delete task 4b2a9c1d-9e0b-7c31-8a2d-4c5d6e7f8a90",
			"Scheduled for next Monday", "Iniciar sessão", "Monday, 4 October 2026",
			"Mon 04 Oct 2026 12:00:00 UTC", "Sat 1 Jan 09:00 UTC",
			"Name:", "e.g.",
			"3 out of 10", "of-PT-or-not",
		}, false},
	} {
		for _, text := range table.texts {
			t.Run(table.what+"/"+text, func(t *testing.T) {
				if got := Exempt(text); got != table.want {
					t.Errorf("Exempt(%q) = %t, want %t", text, got, table.want)
				}
			})
		}
	}
}

func TestCanonFoldsWhitespaceAndTrailingSeparators(t *testing.T) {
	for _, row := range []struct{ in, want string }{
		{"  Save \u00a0 now ", "Save now"},
		{"Delete · ", "Delete"},
		{"A — B", "A — B"},
		{"\n\t Dashboard \n", "Dashboard"},
		{"Item › ", "Item"},
		{"· ", ""},
		{"trail |", "trail"},
		{"Line\u2028break", "Line break"},
	} {
		if got := canon(row.in); got != row.want {
			t.Errorf("canon(%q) = %q, want %q", row.in, got, row.want)
		}
	}
}

// TestScanCollectsWhatAPersonReads is one fixture document asserting the whole list
// of strings the scan hands back: every text node, the four attributes, and nothing
// from a script or a style body — while the node an author hid from assistive
// technology stays in, because hiding a label from a screen reader does not make an
// English word translatable.
func TestScanCollectsWhatAPersonReads(t *testing.T) {
	const document = `<!doctype html><html><head><title>⟦Tasks⟧</title>
<style>body { content: "Hard-coded"; }</style></head>
<body><main><table><tbody><tr><td>2f1e9a70-9e0b-7c31-8a2d-4c5d6e7f8a90</td>
<td>Hard-coded</td></tr></tbody></table>
<img src="/x.png" alt="⟦Acme logo⟧"><input placeholder="Search" aria-label="⟦Filter⟧">
<button title="Save">⟦Save⟧</button><span aria-hidden="true">Hidden English</span>
<script>var x = "Hard-coded";</script></main></body></html>`
	marked := func(text string) bool {
		return strings.HasPrefix(text, "⟦") && strings.HasSuffix(text, "⟧")
	}
	collected, err := Scan([]byte(document), marked)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if want := scanWant; len(collected) != len(want) {
		for _, s := range collected {
			t.Logf("got %+v", s)
		}
		t.Fatalf("collected %d strings, want %d", len(collected), len(want))
	}
	for i, want := range scanWant {
		if got := collected[i]; got != want {
			t.Errorf("string %d = %+v, want %+v", i, got, want)
		}
	}
}

// scanWant is the fixture's expected list, in document order: the whole list, not a
// sample, because a scan that quietly stopped collecting is the failure this case
// exists to catch.
var scanWant = []String{
	{Path: "html[1] > head[1] > title[1]#text", Text: "⟦Tasks⟧", Reached: true},
	{Path: "html[1] > body[2] > main[1] > table[1] > tbody[1] > tr[1] > td[1]#text",
		Text: "2f1e9a70-9e0b-7c31-8a2d-4c5d6e7f8a90"},
	{Path: "html[1] > body[2] > main[1] > table[1] > tbody[1] > tr[1] > td[2]#text",
		Text: "Hard-coded"},
	{Path: "html[1] > body[2] > main[1] > img[2]", Text: "⟦Acme logo⟧", Attribute: "alt", Reached: true},
	{Path: "html[1] > body[2] > main[1] > input[3]", Text: "⟦Filter⟧", Attribute: "aria-label", Reached: true},
	{Path: "html[1] > body[2] > main[1] > input[3]", Text: "Search", Attribute: "placeholder"},
	{Path: "html[1] > body[2] > main[1] > button[4]", Text: "Save", Attribute: "title"},
	{Path: "html[1] > body[2] > main[1] > button[4]#text", Text: "⟦Save⟧", Reached: true},
	{Path: "html[1] > body[2] > main[1] > span[5]#text", Text: "Hidden English"},
}

func TestViolationsAndExemptedSplitTheUnmarked(t *testing.T) {
	collected := []String{
		{Text: "⟦Save⟧", Reached: true},
		{Text: "Save"},
		{Text: "42"},
	}
	if got := Violations(collected); len(got) != 1 || got[0].Text != "Save" {
		t.Errorf("Violations = %+v", got)
	}
	if got := Exempted(collected); len(got) != 1 || got[0].Text != "42" {
		t.Errorf("Exempted = %+v", got)
	}
}
