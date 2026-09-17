package forms_test

import (
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/forms"
)

// valid is the DOM-identity syntax Example refuses outside: a leading ASCII
// letter, then letters, digits, underscores or hyphens.
var valid = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`)

// TestNamespaceDerivesAFormIdentity: the generated screens get one form identity
// per address, and every byte of that identity has to survive being written into
// id="…", hx-target="#…" and hx-select="#…". Each row below is an address a real
// or plausible screen posts to, and the expectation is the identity a person
// would want to read in DevTools.
func TestNamespaceDerivesAFormIdentity(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ action, want string }{
		{"/admin/note/notes", "admin-note-notes"},
		{"/admin/note/notes/1", "admin-note-notes-1"},
		{"/admin/note/notes/0f6f1a2b-3c4d-5e6f-7a8b-9c0d1e2f3a4b/edit", "admin-note-notes-0f6f1a2b-3c4d-5e6f-7a8b-9c0d1e2f3a4b-edit"},
		{"//admin///notes--", "admin-notes"}, // separators collapse and trim
		{"admin?sort=title&dir=asc", "admin-sort-title-dir-asc"},
		{"", "pk-form"},                // nothing usable at all
		{"///", "pk-form"},             // ditto, punctuated
		{"1st/2nd", "pk-1st-2nd"},      // a leading digit is prefixed, not dropped
		{"./rel", "rel"},               // a leading separator is dropped
		{"note_1-edit", "note_1-edit"}, // underscore is legal, so it is kept
		{"/api/v1/email-verification/register", "api-v1-email-verification-register"},
	} {
		if got := forms.Namespace(tc.action); got != tc.want {
			t.Errorf("Namespace(%q) = %q, want %q", tc.action, got, tc.want)
		}
	}
}

// TestNamespaceCannotProduceAnUnusableIdentity. One table cannot close this class:
// any byte at all could arrive in an action, and a namespace that Example refuses
// would leave a served page without a form. So the property is checked over every
// single byte and a sample of combinations, through the constructor that owns the
// rule rather than against a copy of it.
func TestNamespaceCannotProduceAnUnusableIdentity(t *testing.T) {
	t.Parallel()
	check := func(action string) {
		got := forms.Namespace(action)
		if !valid.MatchString(got) {
			t.Errorf("Namespace(%q) = %q, which Example would refuse and no page can target", action, got)
		}
		if _, err := forms.Example("probe", forms.Model{}, forms.Options{Namespace: got}); err != nil {
			t.Errorf("Namespace(%q) = %q, which Example refuses: %v", action, got, err)
		}
	}
	// Every byte, alone and flanked by the separators a path is made of.
	for b := range 256 {
		c := string([]byte{byte(b)}) // Raw bytes, not runes: an action is a path.
		check(c)
		check("/" + c + "/x")
		check(c + c)
	}
	// Multi-byte UTF-8, which is what a non-ASCII path segment actually is.
	for _, s := range []string{"café/menu", "/日本/語", "a/../b", "%2f%2e", "x\x00y"} {
		check(s)
	}
}

// TestNamespaceSeparatesTheFormsOfOneEntity: create and edit are the two screens
// that used to share one identity, and a page holding both could not tell a field
// error of one from a field error of the other.
func TestNamespaceSeparatesTheFormsOfOneEntity(t *testing.T) {
	t.Parallel()
	create := forms.Namespace("/admin/note/notes")
	edit := forms.Namespace("/admin/note/notes/1")
	if create == edit {
		t.Fatalf("create and edit share the identity %q", create)
	}
	// The refused POST is answered by the screen at the address it was posted to,
	// so deriving it twice from one address must give one identity: the swap that
	// puts the errors back targets the id the form was drawn with.
	if again := forms.Namespace("/admin/note/notes"); again != create {
		t.Fatalf("the same address derived %q and then %q; the 422 swap would miss its form", create, again)
	}
}

// TestFormIdentityTravelsFromTheFormToItsControls. The literal legacy ids this
// repository used to assert are gone; what has to hold is the relationship, so it
// is asserted as one: the form's own id, the swap targets that must name it, and
// each label's `for` matching the id of the input it labels.
func TestFormIdentityTravelsFromTheFormToItsControls(t *testing.T) {
	t.Parallel()
	model := forms.Model{Fields: []forms.Field{
		{Definition: entity.Field{Name: "title", Type: "text"}},
		{Definition: entity.Field{Name: "body", Type: "text", Widget: "textarea"}},
	}}
	example, err := forms.Example("form", model, forms.Options{Namespace: "notes-new", Action: "/admin/note/notes"})
	if err != nil {
		t.Fatal(err)
	}
	var html strings.Builder
	if err := example.Node.Render(&html); err != nil {
		t.Fatal(err)
	}
	body := html.String()
	for _, marker := range []string{
		`id="notes-new-form"`, `hx-post="/admin/note/notes"`, `hx-target="#notes-new-form"`,
		`hx-select="#notes-new-form"`,
	} {
		if !strings.Contains(body, marker) {
			t.Errorf("the form lost its own identity: no %s in %s", marker, body)
		}
	}
	for _, name := range []string{"title", "body"} {
		id := "notes-new-field-" + hex.EncodeToString([]byte(name))
		// A label whose `for` names a different element than the input it precedes
		// is read aloud as an unlabeled field, which is the bug the identity exists
		// to prevent rather than a cosmetic one.
		forPattern := `for="` + id + `"`
		idPattern := `id="` + id + `"`
		if !strings.Contains(body, forPattern) || !strings.Contains(body, idPattern) {
			t.Errorf("field %q is not labelled by identity: want %s and %s in %s", name, forPattern, idPattern, body)
		}
		if !strings.Contains(body, `name="`+name+`"`) {
			t.Errorf("field %q does not submit under its own name: %s", name, body)
		}
	}
	// The name is never the identity again: two fields of one entity, drawn on two
	// screens, must not answer to one another.
	if strings.Contains(body, `id="pk-input-title"`) {
		t.Error("the name-derived identity is still in the DOM, so two screens of one entity collide")
	}
}

// TestMustExampleRefusesWhatExampleRefuses. The generated screens call MustExample
// because a renderer returns a page rather than an error, so the one thing it must
// not do is quietly draw something for a schema Example would refuse: two fields of
// one name would render, and the page would carry two controls with one identity.
func TestMustExampleRefusesWhatExampleRefuses(t *testing.T) {
	t.Parallel()
	model := forms.Model{Fields: []forms.Field{
		{Definition: entity.Field{Name: "amount", Type: "text"}},
		{Definition: entity.Field{Name: "amount", Type: "number"}},
	}}
	if _, err := forms.Example("form", model, forms.Options{Namespace: "ledger-new"}); !errors.Is(err, forms.ErrFields) {
		t.Fatalf("Example accepted colliding field names: %v", err)
	}

	var recovered any
	id := "amount"
	defer func() {
		recovered = recover()
		if recovered == nil {
			t.Errorf("MustExample rendered two fields named %q instead of refusing them", id)
			return
		}
		message, _ := recovered.(string)
		for _, want := range []string{"amount", "two fields"} {
			if !strings.Contains(message, want) {
				t.Errorf("MustExample panicked without naming %q: %v", want, recovered)
			}
		}
	}()
	example := forms.MustExample("form", model, forms.Options{Namespace: "ledger-new", Action: "/save"})
	if example.Node != nil {
		t.Fatal("MustExample returned a node past the point it should have refused")
	}
}
