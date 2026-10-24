package resource_test

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/resource"
)

// Person is the entity the users list stands in for: one field that says what it
// is, next to one that says nothing about how it should be read.
type Person struct {
	crud.Base
	Handle string `json:"handle,omitempty" ui:"present:person"`
	Email  string `json:"email"`
}

func (Person) TableName() string { return "people" }

func listOut(t *testing.T, fields []entity.Field, row map[string]any) string {
	t.Helper()
	v := resource.List(resource.Resource{
		// The screen address the kernel would have composed, and which every
		// link into a row is built from.
		Screen: "/app/person/people",
		Schema: entity.Schema{
			Module: "person", Entity: "person", Path: "/api/v1/person/people", Fields: fields,
		},
	}, opts, []map[string]any{row}, 1, 1, "", false)
	return render(t, v.Body)
}

// TestEveryPresentationNameInTheVocabularyComposesSomething is the gate that keeps
// the vocabulary honest: an entry in kit/entity that no renderer honours is a
// declaration that means nothing and looks like it means something, which is the
// failure the write axis had before kit/rest started refusing unknown widgets. It
// iterates the vocabulary rather than naming entries, so adding a word here in
// kit/entity without composing one there fails this test and not a person's page.
func TestEveryPresentationNameInTheVocabularyComposesSomething(t *testing.T) {
	for _, name := range entity.Presentations {
		fields := []entity.Field{
			{Name: "title", Type: entity.TypeString, Present: name},
			{Name: "id", Type: entity.TypeString},
		}
		row := map[string]any{"title": "ada", "id": "1"}
		plainFields := slices.Clone(fields)
		for i := range plainFields {
			plainFields[i].Present = ""
		}
		plain := listOut(t, plainFields, row)
		composed := listOut(t, fields, row)
		if composed == plain {
			t.Errorf("present:%q changed nothing on the list; the vocabulary has an entry no renderer honours", name)
			continue
		}
		if !strings.Contains(composed, "ada") {
			t.Errorf("present:%q composed a cell that lost the value: %s", name, composed)
		}
	}
}

// TestAPersonCellIsADiscAndTheNameSaidOnce. The name is in the props twice and must
// be audible once: a disc that labels itself and a span beside it is a reader saying
// "ada" twice for one row, which is the defect Avatar exists to prevent. The identity
// column is already the way into the record, so a person there composes as the link
// rather than instead of it.
func TestAPersonCellIsADiscAndTheNameSaidOnce(t *testing.T) {
	out := listOut(t, entity.Fields[*Person](), map[string]any{"handle": "ada", "email": "ada@acme.test", "id": "1"})
	if !strings.Contains(out, "rounded-full") {
		t.Fatalf("no disc in the person cell: %s", out)
	}
	if n := strings.Count(out, ">ada<"); n != 1 {
		t.Errorf("the name is read %d times, want once: %s", n, out)
	}
	// The primary disc is an anchor (it is the way in) and a plain one is a div, so
	// the assertion is about whichever element carries the class.
	disc := regexp.MustCompile(`<(?:div|a) class="[^"]*rounded-full[^"]*"[^>]*>`)
	if m := disc.FindString(out); !strings.Contains(m, `aria-hidden="true"`) {
		t.Errorf("with the name beside it the disc must be quiet; got %q", m)
	}
	if strings.Contains(out, `role="img"`) {
		t.Error("a disc whose name is in the adjacent text must not also be an image to a reader")
	}
	// The way in survives the composition. A cell that grew a disc and lost its
	// href took the mouse away from the person using the screen.
	if !strings.Contains(out, `href="/app/person/people/1"`) {
		t.Errorf("the person cell lost the link that was already there: %s", out)
	}
}

// TestAFieldThatNamesNothingIsRenderedExactlyAsBefore, which is the claim that let
// this go near every other screen in the repository: the slot returns nil and the
// table draws the value itself.
func TestAFieldThatNamesNothingIsRenderedExactlyAsBefore(t *testing.T) {
	fields := entity.Fields[*Person]()
	for i := range fields {
		fields[i].Present = ""
	}
	row := map[string]any{"handle": "ada", "email": "ada@acme.test", "id": "1"}
	if got, want := listOut(t, fields, row), listOut(t, entity.Fields[*Person](), row); got == want {
		t.Error("the test cannot tell composed and plain apart; it proves nothing")
	}
	plain := listOut(t, fields, row)
	for _, want := range []string{"ada", "ada@acme.test"} {
		if !strings.Contains(plain, want) {
			t.Errorf("plain rendering lost %q: %s", want, plain)
		}
	}
	if strings.Contains(plain, "rounded-full") {
		t.Errorf("a field naming no presentation grew a disc: %s", plain)
	}
}

// TestAnUnclaimedPersonIsADashAndNotAFacelessDisc: an empty handle is a field
// nobody filled in, and every other empty field on this screen says so with a dash.
// Composing a generic person glyph there would claim the screen knows whose face is
// missing.
func TestAnUnclaimedPersonIsADashAndNotAFacelessDisc(t *testing.T) {
	out := listOut(t, entity.Fields[*Person](), map[string]any{"handle": "", "email": "ada@acme.test", "id": "1"})
	if strings.Contains(out, "rounded-full") {
		t.Errorf("a person who claimed nothing grew a disc: %s", out)
	}
	if !strings.Contains(out, "—") {
		t.Errorf("an unclaimed field should read as empty, the way every other empty field does: %s", out)
	}
}
