package screens_test

// hints_test.go is the contract test for the reading hints: the bytes a hinted
// entry serialises to, the bytes an un-hinted one still serialises to, and the
// tags that declare the field level.
//
// The first case is the frozen document. It is compared key for key against a
// literal rather than as a diff blob, so that a renamed key fails by name —
// `summaryFields` spelled `summary` is the mistake this file exists to refuse,
// and a shell in a pocket parses the answer.

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/ui/screens"
)

// hintedFields is the fixture's schema, written as the schema arrives from a
// struct: the field level wears its hints on entity.Field, so every carrier from
// here to the document is one this repository already had.
func hintedFields() []entity.Field {
	return []entity.Field{
		{Name: "id", Type: entity.TypeUUID, ReadOnly: true},
		{Name: "title", Type: entity.TypeString, Required: true, Doc: "What this note is about",
			Presentation: entity.FieldHints{Label: "Subject", Help: "One line a person will recognise later", Visibility: "shown"}},
		{Name: "body", Type: entity.TypeText, Widget: "textarea", HideList: true,
			Presentation: entity.FieldHints{Section: "record"}},
		{Name: "status", Type: entity.TypeString, Widget: "select", Doc: "Lifecycle state",
			Enum: []string{"open", "in_progress", "resolved"},
			Presentation: entity.FieldHints{
				EnumLabels: map[string]string{"in_progress": "In progress", "open": "Open", "resolved": "Resolved"},
				EnumTones:  map[string]string{"in_progress": "info", "open": "neutral", "resolved": "success"}}},
		{Name: "authorId", Type: entity.TypeUUID,
			Presentation: entity.FieldHints{Label: "Author", Section: "record", Visibility: "detail",
				Reference: &entity.FieldReference{Resource: "user.user"}}},
		{Name: "retainerFee", Type: entity.TypeInt,
			Presentation: entity.FieldHints{Label: "Retainer", Section: "record", Format: "money",
				Money: &entity.FieldMoney{CurrencyField: "currency", Scale: 2}}},
		{Name: "currency", Type: entity.TypeString,
			Presentation: entity.FieldHints{Visibility: "hidden"}},
		{Name: "sentAt", Type: entity.TypeTime,
			Presentation: entity.FieldHints{Section: "record", Format: "relative"}},
	}
}

func hinted() httpx.Resource {
	r := resource()
	r.Schema.Fields = hintedFields()
	r.Present = entity.EntryHints{
		Singular: "note", Plural: "notes",
		Description:  "What the firm wrote down about a client",
		Icon:         "document",
		Group:        &entity.ResourceGroup{Key: "practice", Label: "Practice"},
		Order:        10,
		PrimaryField: "title", PreviewField: "body",
		SummaryFields: []string{"status", "sentAt"}, StatusField: "status",
		Sections:         []entity.EntitySection{{Key: "record", Label: "Record information"}},
		EmptyDescription: "Nothing has been written down yet.",
		Sortable:         []string{"title", "sentAt"},
	}
	r.Commands = []httpx.Command{
		{Verb: "publish", Summary: "Publish a note", Collection: false,
			Description: "Makes the note visible. Publishing a published note changes nothing.",
			Fields:      []entity.Field{{Name: "at", Type: entity.TypeTime, Doc: "When it goes out; now if left empty"}},
			Present: entity.CommandHints{Label: "Publish", Primary: true,
				SuccessMessage: "The note is published."}},
		{Verb: "archive", Summary: "Archive every resolved note", Collection: true,
			Description: "Takes what is finished out of the way.",
			Present: entity.CommandHints{Label: "Archive resolved", Destructive: true,
				Confirmation: &entity.CommandConfirmation{Title: "Archive every resolved note?",
					Body: "Resolved notes leave the list. Nothing else changes.", ConfirmLabel: "Archive"},
				SuccessMessage: "The resolved notes are archived."}},
		{Verb: "purge-drafts", Summary: "Purge abandoned drafts",
			Description: "An operator's door, not a person's.",
			Present:     entity.CommandHints{System: true}},
	}
	return r
}

// TestEveryHintSerialisesToTheFrozenJSON is the contract: the document a hinted
// resource answers with, byte for byte.
func TestEveryHintSerialisesToTheFrozenJSON(t *testing.T) {
	t.Parallel()
	got, err := json.MarshalIndent(screens.Describe1(hinted(), true), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != frozenEntry {
		t.Errorf("the hinted entry is not the frozen document.\ngot:\n%s\n\nwant:\n%s", got, frozenEntry)
	}
}

// frozenEntry is the document §3.1 of the specification freezes, as a literal
// and not a shape: a key the contract does not name fails here, in either
// direction.
const frozenEntry = `{
  "module": "note",
  "entity": "note",
  "path": "/api/v1/note/notes",
  "fields": [
    {
      "name": "id",
      "type": "uuid",
      "readOnly": true
    },
    {
      "name": "title",
      "type": "string",
      "required": true,
      "doc": "What this note is about",
      "presentation": {
        "label": "Subject",
        "help": "One line a person will recognise later",
        "visibility": "shown"
      }
    },
    {
      "name": "body",
      "type": "text",
      "widget": "textarea",
      "hideList": true,
      "presentation": {
        "section": "record"
      }
    },
    {
      "name": "status",
      "type": "string",
      "widget": "select",
      "enum": [
        "open",
        "in_progress",
        "resolved"
      ],
      "doc": "Lifecycle state",
      "presentation": {
        "enumLabels": {
          "in_progress": "In progress",
          "open": "Open",
          "resolved": "Resolved"
        },
        "enumTones": {
          "in_progress": "info",
          "open": "neutral",
          "resolved": "success"
        }
      }
    },
    {
      "name": "authorId",
      "type": "uuid",
      "presentation": {
        "label": "Author",
        "section": "record",
        "visibility": "detail",
        "reference": {
          "resource": "user.user"
        }
      }
    },
    {
      "name": "retainerFee",
      "type": "int",
      "presentation": {
        "label": "Retainer",
        "section": "record",
        "format": "money",
        "money": {
          "currencyField": "currency",
          "scale": 2
        }
      }
    },
    {
      "name": "currency",
      "type": "string",
      "presentation": {
        "visibility": "hidden"
      }
    },
    {
      "name": "sentAt",
      "type": "time",
      "presentation": {
        "section": "record",
        "format": "relative"
      }
    }
  ],
  "presentation": {
    "singular": "note",
    "plural": "notes",
    "description": "What the firm wrote down about a client",
    "icon": "document",
    "group": {
      "key": "practice",
      "label": "Practice"
    },
    "order": 10,
    "primaryField": "title",
    "previewField": "body",
    "summaryFields": [
      "status",
      "sentAt"
    ],
    "statusField": "status",
    "sections": [
      {
        "key": "record",
        "label": "Record information"
      }
    ],
    "emptyDescription": "Nothing has been written down yet.",
    "sortable": [
      "title",
      "sentAt"
    ]
  },
  "screen": "/app/note/notes",
  "immutable": [
    "status"
  ],
  "writable": true,
  "commands": [
    {
      "verb": "publish",
      "summary": "Publish a note",
      "description": "Makes the note visible. Publishing a published note changes nothing.",
      "fields": [
        {
          "name": "at",
          "type": "time",
          "doc": "When it goes out; now if left empty"
        }
      ],
      "presentation": {
        "label": "Publish",
        "primary": true,
        "successMessage": "The note is published."
      }
    },
    {
      "verb": "archive",
      "summary": "Archive every resolved note",
      "description": "Takes what is finished out of the way.",
      "collection": true,
      "presentation": {
        "label": "Archive resolved",
        "destructive": true,
        "confirmation": {
          "title": "Archive every resolved note?",
          "body": "Resolved notes leave the list. Nothing else changes.",
          "confirmLabel": "Archive"
        },
        "successMessage": "The resolved notes are archived."
      }
    },
    {
      "verb": "purge-drafts",
      "summary": "Purge abandoned drafts",
      "description": "An operator's door, not a person's.",
      "presentation": {
        "system": true
      }
    }
  ]
}`

// TestTheEntryLevelKeysAreTheFrozenOnes names every key of the entry block the
// contract froze, so a rename fails here by name and not as a diff.
func TestTheEntryLevelKeysAreTheFrozenOnes(t *testing.T) {
	t.Parallel()
	got, err := json.Marshal(screens.Describe1(hinted(), true).Presentation)
	if err != nil {
		t.Fatal(err)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(got, &keys); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"singular", "plural", "description", "icon", "group", "order",
		"primaryField", "previewField", "summaryFields", "statusField", "sections",
		"emptyDescription", "sortable"} {
		if _, ok := keys[want]; !ok {
			t.Errorf("the entry block carries no %q key", want)
		}
	}
	if len(keys) != 13 {
		t.Errorf("the entry block carries %d keys, want the 13 the contract froze: %s", len(keys), got)
	}
}

func TestTheCommandLevelKeysAreTheFrozenOnes(t *testing.T) {
	t.Parallel()
	commands := map[string]map[string]json.RawMessage{}
	for _, c := range screens.Describe1(hinted(), true).Commands {
		raw, err := json.Marshal(c.Presentation)
		if err != nil {
			t.Fatal(err)
		}
		var keys map[string]json.RawMessage
		if err := json.Unmarshal(raw, &keys); err != nil {
			t.Fatal(err)
		}
		commands[c.Verb] = keys
	}
	for verb, want := range map[string][]string{
		"publish":      {"label", "primary", "successMessage"},
		"archive":      {"label", "destructive", "confirmation", "successMessage"},
		"purge-drafts": {"system"},
	} {
		keys, ok := commands[verb]
		if !ok {
			t.Fatalf("the document carries no %q command", verb)
		}
		if len(keys) != len(want) {
			t.Errorf("%s carries %d keys, want %v: %v", verb, len(keys), want, keys)
			continue
		}
		for _, key := range want {
			if _, ok := keys[key]; !ok {
				t.Errorf("%s carries no %q key", verb, key)
			}
		}
	}
	// `system` is declared on one command only, so it prints there and nowhere
	// else: an author's silence is not the server's opinion about the default.
	if _, ok := commands["publish"]["system"]; ok {
		t.Error("publish prints a system hint nobody declared for it")
	}
}

func TestTheFieldLevelKeysAreTheFrozenOnes(t *testing.T) {
	t.Parallel()
	entry := screens.Describe1(hinted(), true)
	byName := map[string]json.RawMessage{}
	for _, f := range entry.Fields {
		raw, err := json.Marshal(f)
		if err != nil {
			t.Fatal(err)
		}
		var all map[string]json.RawMessage
		if err := json.Unmarshal(raw, &all); err != nil {
			t.Fatal(err)
		}
		byName[f.Name] = all["presentation"]
	}
	// Every key the brief froze, on the field that declares it.
	for name, want := range map[string][]string{
		"title":       {"label", "help", "visibility"},
		"body":        {"section"},
		"status":      {"enumLabels", "enumTones"},
		"authorId":    {"label", "section", "visibility", "reference"},
		"retainerFee": {"label", "section", "format", "money"},
		"currency":    {"visibility"},
		"sentAt":      {"section", "format"},
	} {
		raw := byName[name]
		if len(raw) == 0 {
			t.Fatalf("field %q carries no presentation block at all", name)
		}
		var keys map[string]json.RawMessage
		if err := json.Unmarshal(raw, &keys); err != nil {
			t.Fatalf("field %q: %v", name, err)
		}
		if len(keys) != len(want) {
			t.Errorf("field %q carries %s, want the %d keys %v", name, raw, len(want), want)
			continue
		}
		for _, key := range want {
			if _, ok := keys[key]; !ok {
				t.Errorf("field %q carries no %q key: %s", name, key, raw)
			}
		}
	}
	// And the field nobody hinted prints no key: this is the same rule one level
	// down, and the id is the row's identity, which nobody ever hinted.
	if len(byName["id"]) != 0 {
		t.Errorf("the unhinted id field prints a presentation block: %s", byName["id"])
	}
}

// TestAnUnhintedEntryIsByteForByteUnchanged is acceptance criterion two. The
// golden on disk is the document every shell already parsed; the assertion is
// that nothing this delivery added appears in it, in any shape — including an
// empty `"presentation": {}`, which a defaulted struct would print for every
// entry and which no installed parser has ever seen.
func TestAnUnhintedEntryIsByteForByteUnchanged(t *testing.T) {
	t.Parallel()
	onDisk, err := os.ReadFile("testdata/catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(onDisk, []byte(`"presentation"`)) {
		t.Fatal("the committed catalogue golden carries a presentation block; an un-hinted entry must print none")
	}
	for _, r := range []httpx.Resource{resource(), hinted()} {
		if !reflect.DeepEqual(r.Present, entity.EntryHints{}) {
			continue
		}
		got, err := json.Marshal(screens.Describe1(r, true))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(got, []byte(`"presentation"`)) {
			t.Errorf("an entry declaring nothing prints a presentation block: %s", got)
		}
	}
	// The golden is still what the fixture describes, so a regeneration cannot
	// silence the check above by writing the new bytes into the file.
	var golden screens.Catalog
	if err := json.Unmarshal(onDisk, &golden); err != nil {
		t.Fatal(err)
	}
	if len(golden.Resources) == 0 {
		t.Fatalf("the golden holds %d entries", len(golden.Resources))
	}
	for _, e := range golden.Resources {
		if e.Presentation != nil {
			t.Errorf("%s.%s carries hints in the golden, which holds no hinted resource", e.Module, e.Entity)
		}
	}
}

// hintedTags is the field level as a module would actually write it. derive is
// the one parser, and what it must answer is the same FieldHints the case above
// wrote by hand — one implementation, two declaration shapes.
type hintedTags struct {
	ID       string `json:"id"`
	Title    string `json:"title" ui:"label:Subject;help:One line a person will recognise later;visibility:shown"`
	Body     string `json:"body" ui:"widget:textarea;hide:list;section:record"`
	Status   string `json:"status" enum:"open,in_progress,resolved" ui:"widget:select" enumLabels:"open=Open,in_progress=In progress,resolved=Resolved" enumTones:"open=neutral,in_progress=info,resolved=success"`
	AuthorID string `json:"authorId" ui:"label:Author;section:record;visibility:detail;reference:user.user"`
	Fee      int64  `json:"retainerFee" ui:"label:Retainer;section:record;format:money;currency:currency;scale:2"`
	Currency string `json:"currency" ui:"visibility:hidden"`
	SentAt   string `json:"sentAt" ui:"section:record;format:relative"`
}

func TestTagsDeclareTheSameHintsAsHandWrittenFields(t *testing.T) {
	t.Parallel()
	tagged := crud.FieldsOf(reflect.TypeFor[hintedTags]())
	byName := map[string]entity.Field{}
	for _, f := range tagged {
		byName[f.Name] = f
	}
	for _, want := range hintedFields() {
		got, ok := byName[want.Name]
		if !ok {
			t.Fatalf("derive answered no field %q", want.Name)
		}
		if got.Presentation.Label != want.Presentation.Label ||
			got.Presentation.Help != want.Presentation.Help ||
			got.Presentation.Section != want.Presentation.Section ||
			got.Presentation.Visibility != want.Presentation.Visibility ||
			got.Presentation.Format != want.Presentation.Format {
			t.Errorf("field %q derives %+v, want %+v", want.Name, got.Presentation, want.Presentation)
		}
		if !reflect.DeepEqual(got.Presentation.EnumLabels, want.Presentation.EnumLabels) {
			t.Errorf("field %q derives enum labels %v, want %v", want.Name, got.Presentation.EnumLabels, want.Presentation.EnumLabels)
		}
		if !reflect.DeepEqual(got.Presentation.EnumTones, want.Presentation.EnumTones) {
			t.Errorf("field %q derives enum tones %v, want %v", want.Name, got.Presentation.EnumTones, want.Presentation.EnumTones)
		}
		if !reflect.DeepEqual(got.Presentation.Reference, want.Presentation.Reference) {
			t.Errorf("field %q derives reference %+v, want %+v", want.Name, got.Presentation.Reference, want.Presentation.Reference)
		}
		if !reflect.DeepEqual(got.Presentation.Money, want.Presentation.Money) {
			t.Errorf("field %q derives money %+v, want %+v", want.Name, got.Presentation.Money, want.Presentation.Money)
		}
	}
}
