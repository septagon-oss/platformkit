package resource_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/resource"
)

// TestAListIsCalledByTheWordsItsEntryWrote pins that the count a list prints uses
// the entry's own singular and plural, not the declared entity name's pair.
func TestAListIsCalledByTheWordsItsEntryWrote(t *testing.T) {
	t.Parallel()
	r := note()
	r.Present = entity.EntryHints{Singular: "memo", Plural: "memos"}
	one := render(t, resource.List(r, opts, []map[string]any{{"id": "1", "title": "a"}}, 1, 1, "", false).Body)
	if !strings.Contains(one, "1 memo") || strings.Contains(one, "1 note") {
		t.Errorf("the count does not use the entry's words:\n%s", one)
	}
	many := render(t, resource.List(r, opts, nil, 3, 1, "", false).Body)
	if !strings.Contains(many, "3 memos") {
		t.Errorf("the plural does not use the entry's words:\n%s", many)
	}
}
