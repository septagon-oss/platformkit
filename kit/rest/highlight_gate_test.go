package rest

import (
	"strings"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
)

// highlight_gate_test.go asks the mount gate the four questions a declared
// sub-count can get wrong, one per case, because a single table would pass with
// any one clause of the gate deleted. Each refusal is a sentence a module author
// can act on: the alternative is a card that says "Due today" over a number that
// was not asked about.

// Highlighted is the fixture: a title, a moment, a count and a list.
type Highlighted struct {
	crud.Base
	Title    string     `json:"title"`
	DueAt    *time.Time `json:"dueAt,omitempty"`
	Open     int        `json:"open,omitempty"`
	Flags    string     `json:"flags,omitempty"`
	Tag      []string   `json:"tag,omitempty" gorm:"-"`
	Untagged string     `json:"untagged,omitempty" ui:"visibility:hidden"`
}

func (Highlighted) TableName() string { return "rest_highlighted" }

// dueToday is one sound declaration, and the baseline every case below is one
// word away from: a refusal that fires on a coherent declaration is worse at
// mount than one that never fires at all.
func dueToday() *entity.EntryHighlight {
	return &entity.EntryHighlight{
		Label: "Due today",
		Filter: []entity.EntryPredicate{
			{Field: "dueAt", Op: "gte", Value: "today"},
			{Field: "dueAt", Op: "lt", Value: "tomorrow"},
		},
	}
}

func faultOf(h *entity.EntryHighlight, trend string) string {
	return entryHintsFault("thing", entity.EntryHints{Highlight: h, TrendField: trend}, crud.Fields[*Highlighted]())
}

func TestTheMountRefusesAHighlightThatNamesNothing(t *testing.T) {
	for _, tc := range []struct {
		name string
		h    *entity.EntryHighlight
		want string
	}{
		{"no predicate at all", &entity.EntryHighlight{Label: "Due today"}, "the total wearing a label"},
		{"no label", &entity.EntryHighlight{Filter: dueToday().Filter}, "no label"},
		{"a field the entity has not got", &entity.EntryHighlight{Label: "x",
			Filter: []entity.EntryPredicate{{Field: "notAColumn", Op: "eq", Value: "y"}}}, "not a field"},
		{"a list column", &entity.EntryHighlight{Label: "x",
			Filter: []entity.EntryPredicate{{Field: "tag", Op: "eq", Value: "y"}}}, "is a list"},
		{"an operator the grammar has not got", &entity.EntryHighlight{Label: "x",
			Filter: []entity.EntryPredicate{{Field: "dueAt", Op: "contains", Value: "today"}}}, "no operator"},
		{"a range over words", &entity.EntryHighlight{Label: "x",
			Filter: []entity.EntryPredicate{{Field: "title", Op: "lt", Value: "z"}}}, "compares a string"},
		{"a moment written as prose", &entity.EntryHighlight{Label: "x",
			Filter: []entity.EntryPredicate{{Field: "dueAt", Op: "lt", Value: "next tuesday"}}}, "is not one"},
		{"a count written as prose", &entity.EntryHighlight{Label: "x",
			Filter: []entity.EntryPredicate{{Field: "open", Op: "gt", Value: "several"}}}, "is not one"},
		{"bounds that admit nothing", &entity.EntryHighlight{Label: "Due today",
			Filter: []entity.EntryPredicate{
				{Field: "dueAt", Op: "gte", Value: "today"},
				{Field: "dueAt", Op: "lt", Value: "today"},
			}}, "admits nothing"},
	} {
		if bad := faultOf(tc.h, ""); !strings.Contains(bad, tc.want) {
			t.Errorf("%s: the gate answers %q, which does not say %q", tc.name, bad, tc.want)
		}
	}
	if bad := faultOf(dueToday(), ""); bad != "" {
		t.Errorf("the sound declaration is refused: %s", bad)
	}
}

func TestTheMountRefusesARangeOperatorOnAStringField(t *testing.T) {
	bad := faultOf(&entity.EntryHighlight{Label: "Started before",
		Filter: []entity.EntryPredicate{{Field: "flags", Op: "lte", Value: "m"}},
	}, "")
	if !strings.Contains(bad, "compares a string") {
		t.Errorf("an order asked of words is served: %q", bad)
	}
	// The same field with the one comparison a word admits mounts: the refusal is
	// about the operator and not about the column.
	if bad := faultOf(&entity.EntryHighlight{Label: "Flagged",
		Filter: []entity.EntryPredicate{{Field: "flags", Op: "eq", Value: "m"}}}, ""); bad != "" {
		t.Errorf("a flag asked by name is refused: %s", bad)
	}
}

func TestTheMountRefusesATrendFieldThatIsNotATimestamp(t *testing.T) {
	if bad := faultOf(nil, "title"); !strings.Contains(bad, "column of days") {
		t.Errorf("a day breakdown of a title is served: %q", bad)
	}
	if bad := faultOf(nil, "notAColumn"); !strings.Contains(bad, "not a field") {
		t.Errorf("a trend named against nothing is served: %q", bad)
	}
	if bad := faultOf(nil, "dueAt"); bad != "" {
		t.Errorf("a trend over the entity's own moment is refused: %s", bad)
	}
}

func TestTheMountRefusesAHighlightOnAResourceWithNoList(t *testing.T) {
	// The Spec-level question: the closures a declared highlight and a trendField
	// ask are refused for a resource that mounts no list, so the declaration here
	// would name a question nothing would ever answer. Spec.check is reached at
	// Mount, which is where a Spec has its entity, its schema and its operations
	// all in hand.
	s := Spec[*Highlighted]{Module: "rest", Entity: "thing", Path: "things", Read: "rest:read", Write: "rest:write"}
	s.Present = entity.EntryHints{Highlight: dueToday(), TrendField: "dueAt"}
	// An empty operation set is all five verbs, so the refusal case has to name
	// the verbs it mounted rather than name none: a read with no list is the
	// Singleton shape, and it is the one that cannot answer a count.
	s.Operations = []httpx.CRUD{httpx.CRUDRead}
	bad := s.entryHintFault()
	if !strings.Contains(bad, "list") {
		t.Errorf("a sub-count with no list to answer it is served: %q", bad)
	}
	s.Operations = []httpx.CRUD{httpx.CRUDList}
	if bad := s.entryHintFault(); bad != "" {
		t.Errorf("a sub-count on a resource that mounts a list is refused: %s", bad)
	}
}
