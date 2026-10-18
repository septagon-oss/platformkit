package resource_test

// entry_language_test.go is the half of the copy seam that the vocabulary does
// not speak: which parts of an entry's reading a request's language may change.
//
// ui/resource/hints.go opens by naming the words and then the names — `icon`,
// `primaryField`, `statusField`, `coverField` — and the seam's whole promise is
// that the first list is translated and the second is carried. A resolver built
// by listing the fields it knows honours that promise only until the entry gains
// one more hint: the unlisted field comes back as nothing, in every language, in
// the one document a shell builds its screens from. So this file asks the question
// once, of every field the type holds, rather than of the handful anyone
// remembered.

import (
	"reflect"
	"slices"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/resource"
)

// readAsWords are the entry hints a person reads, and so the only ones a request's
// language may answer for. Every other field of entity.EntryHints names something
// a screen draws, and translating `warning` or `coverImageId` would be a bug and
// not a localisation.
var readAsWords = map[string]bool{
	"Singular": true, "Plural": true, "Description": true, "EmptyDescription": true,
}

// labelCarriers are the two declared blocks that hold a word inside a name: the
// group and each section carry a key a shell groups by and a label a person reads.
// They are checked below on their own, because half of each is a word.
var labelCarriers = map[string]bool{"Group": true, "Sections": true}

// fullEntry is one declaration with every field of entity.EntryHints written,
// because an empty field is a field this gate cannot see: reflect.Value.IsZero on
// the answer is only a failure when something was asked for.
func fullEntry() entity.EntryHints {
	return entity.EntryHints{
		Singular: "note", Plural: "notes", Description: "A thing to do",
		Icon: "document", Group: &entity.ResourceGroup{Key: "work", Label: "Work"},
		Order: 3, PrimaryField: "title", PreviewField: "body",
		SummaryFields: []string{"kind"}, StatusField: "status",
		CoverField: "coverImageId", ThumbnailField: "coverImageId",
		PrimaryCommand:   "assign",
		Sections:         []entity.EntitySection{{Key: "detail", Label: "Detail"}},
		EmptyDescription: "Nothing is due",
		Sortable:         []string{"title", "updatedAt"},
	}
}

// TestEveryEntryHintThatIsNotAWordReachesTheTranslatedEntry asks the seam of one
// declaration in a language that answers every key it is asked about, and then
// walks the type: each word comes back translated, and every other field — each
// name, each number, each list of names — comes back exactly as it was declared.
// It also asks that the shared declaration was not written to, since the entry
// hints ride on the registration and every request reads the same value.
func TestEveryEntryHintThatIsNotAWordReachesTheTranslatedEntry(t *testing.T) {
	declared := fullEntry()
	var asked []string
	words := resource.WordsFor(entity.Schema{Module: "notes", Entity: "note"},
		func(key, fallback string) string {
			asked = append(asked, key)
			return "pt:" + fallback
		})
	got := words.Entry(declared)

	have := reflect.ValueOf(got)
	want := reflect.ValueOf(declared)
	for i := range have.NumField() {
		name := have.Type().Field(i).Name
		if labelCarriers[name] {
			continue
		}
		if readAsWords[name] {
			if wantWord := "pt:" + want.Field(i).String(); have.Field(i).String() != wantWord {
				t.Errorf("%s = %q, want the translation %q", name, have.Field(i).String(), wantWord)
			}
			continue
		}
		if !reflect.DeepEqual(have.Field(i).Interface(), want.Field(i).Interface()) {
			t.Errorf("%s = %#v, want the declaration %#v: a name a screen draws is not a word a person reads, and a hint the resolver did not carry reaches a shell as nothing",
				name, have.Field(i).Interface(), want.Field(i).Interface())
		}
	}

	// The two labels that ride inside a name — the group's and each section's —
	// are words, and their keys are not: a shell groups by the key, so the value
	// that comes back carries one translated half and one untouched one.
	if got.Group == nil || got.Group.Label != "pt:Work" || got.Group.Key != "work" {
		t.Errorf("group = %+v, want the label translated and the key kept", got.Group)
	}
	if len(got.Sections) != 1 || got.Sections[0].Label != "pt:Detail" || got.Sections[0].Key != "detail" {
		t.Errorf("sections = %+v, want the label translated and the key kept", got.Sections)
	}
	if declared.Group.Label != "Work" || declared.Sections[0].Label != "Detail" {
		t.Error("the request's language was written into the shared declaration")
	}

	// And the keys the seam was asked under are the derived ones, so a renamed
	// scheme is caught here rather than by a translation nobody resolves.
	for _, key := range []string{
		"hints.notes/note.singular", "hints.notes/note.plural",
		"hints.notes/note.description", "hints.notes/note.emptyDescription",
		"hints.notes/note.group.work", "hints.notes/note.section.detail",
	} {
		if !slices.Contains(asked, key) {
			t.Errorf("the seam was never asked for %s, so no translation of it can be read", key)
		}
	}
}
