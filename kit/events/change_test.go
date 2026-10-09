package events_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/events"
)

// row is a fixture with one field of each shape the rules name: a plain value, a
// secret tagged for digest only, a field nothing may record, an embedded column, a
// pointer, and a slice.
type row struct {
	ID       uuid.UUID  `json:"id"`
	Tagline  string     `json:"tagline,omitempty"`
	Home     string     `json:"homeSlug,omitempty"`
	Hidden   string     `json:"hidden" audit:"-"`
	Secret   string     `json:"secret" audit:"digest"`
	Revision int64      `json:"revision"`
	Logo     *uuid.UUID `json:"logoFileId,omitempty"`
	At       time.Time  `json:"at"`
}

// TestChangeNamesThePayloadsOwnNames is rule 3: the diff speaks json, because the
// trail is read as json.
func TestChangeNamesThePayloadsOwnNames(t *testing.T) {
	before := &row{Tagline: "Original review tagline", Home: "welcome"}
	after := &row{Tagline: "Replacement review tagline", Home: "welcome"}
	got, err := events.Changes(before, after, "tagline", "homeSlug")
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("the diff carried %+v, want only the tagline", got)
	}
	if got[0].Field != "tagline" {
		t.Errorf("the change is named %q, want the payload's own name", got[0].Field)
	}
	var from, to string
	if err := json.Unmarshal(got[0].Before, &from); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(got[0].After, &to); err != nil {
		t.Fatal(err)
	}
	if from != "Original review tagline" || to != "Replacement review tagline" {
		t.Errorf("the change moved %q to %q, want both halves verbatim", from, to)
	}
}

// TestChangeRefusesANameTheTypeDoesNotHave is rule 5: a list that quietly stopped
// covering a field would explain nothing, so the name is refused rather than skipped.
func TestChangeRefusesANameTheTypeDoesNotHave(t *testing.T) {
	_, err := events.Changes(&row{}, &row{}, "tagline2")
	if err == nil || !strings.Contains(err.Error(), `"tagline2"`) {
		t.Fatalf("an unknown field name answered %v, want an error naming it", err)
	}
	if _, err := events.Changes(&row{}, &row{}); err == nil {
		t.Fatal("a diff over no fields was accepted, which is a change that says nothing")
	}
	if _, err := events.Changes(&row{}, nil, "tagline"); err == nil {
		t.Fatal("a diff with no after row was accepted")
	}
}

// TestChangeRefusesAFieldNothingMayRecord is the disclosure rule: a field its owner
// excluded cannot be pulled into a trail by naming it.
func TestChangeRefusesAFieldNothingMayRecord(t *testing.T) {
	if _, err := events.Changes(&row{Hidden: "a"}, &row{Hidden: "b"}, "hidden"); err == nil {
		t.Fatal("a field tagged audit:\"-\" was diffed")
	}
}

// TestChangeCarriesADigestAndNotTheValue is rule 6: the field is named, both halves
// are comparable, and the value is not in the payload an administrator reads for a
// year. The digest is of the marshalled json, so the same value digests the same.
func TestChangeCarriesADigestAndNotTheValue(t *testing.T) {
	before := &row{Secret: "hunter1"}
	after := &row{Secret: "hunter2"}
	got, err := events.Changes(before, after, "secret")
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	body, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"hunter1", "hunter2"} {
		if strings.Contains(string(body), leak) {
			t.Errorf("the digest entry carries the value %q", leak)
		}
	}
	var halves []string
	for _, raw := range []json.RawMessage{got[0].Before, got[0].After} {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			t.Fatalf("a digest half is not a json string: %v", err)
		}
		if !strings.HasPrefix(s, "sha256:") || len(s) != len("sha256:")+64 || strings.ToLower(s) != s {
			t.Errorf("the digest is spelled %q, want sha256:<64 lower-case hex>", s)
		}
		halves = append(halves, s)
	}
	if halves[0] == halves[1] {
		t.Error("two different values digested alike")
	}
	again, err := events.Changes(&row{Secret: "hunter1"}, &row{Secret: "hunter2"}, "secret")
	if err != nil {
		t.Fatal(err)
	}
	if string(again[0].Before) != string(got[0].Before) {
		t.Error("the same value digested differently, so a holder cannot confirm which one it was")
	}
	// A digest field whose value did not move is not a change, even though its
	// digest is comparable.
	quiet, err := events.Changes(&row{Secret: "hunter1"}, &row{Secret: "hunter1"}, "secret")
	if err != nil {
		t.Fatal(err)
	}
	if len(quiet) != 0 {
		t.Errorf("an unchanged secret produced %+v, want nothing", quiet)
	}
}

// TestChangeSeesThroughAnEmbeddedRow, a pointer and a slice: the names on the row are
// the payload's, wherever the Go type keeps them, and a value compared by its json is
// compared the way the payload will be read.
func TestChangeSeesThroughAnEmbeddedRow(t *testing.T) {
	logo := uuid.New()
	other := uuid.New()
	before := &row{Revision: 3, Logo: &logo, At: time.Unix(1, 0).UTC()}
	after := &row{Revision: 4, Logo: &other, At: time.Unix(1, 0).UTC()}
	got, err := events.Changes(before, after, "revision", "logoFileId", "at")
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("the diff carried %+v, want the revision and the logo and not the instant", got)
	}
	if got[0].Field != "revision" || got[1].Field != "logoFileId" {
		t.Errorf("the diff named %q and %q", got[0].Field, got[1].Field)
	}
	// A pointer set from nothing is a change with no before half to read.
	moved, err := events.Changes(&row{}, &row{Logo: &logo}, "logoFileId")
	if err != nil {
		t.Fatal(err)
	}
	if len(moved) != 1 || len(moved[0].Before) != 0 {
		t.Errorf("a logo appearing from nowhere produced %+v", moved)
	}
}

// TestChangeOverACreateCarriesOnlyWhatTheNewRowHolds: before == nil is a create, and
// a field the new row leaves empty is not news.
func TestChangeOverACreateCarriesOnlyWhatTheNewRowHolds(t *testing.T) {
	got, err := events.Changes(nil, &row{Tagline: "We make things", Revision: 1}, "tagline", "homeSlug", "revision")
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("the create carried %+v, want the tagline and the revision", got)
	}
	for _, c := range got {
		if len(c.Before) != 0 {
			t.Errorf("a create named %s with a before half: %s", c.Field, c.Before)
		}
	}
}

// TestChangeMarshalsAsTheTrailStoresIt pins the payload shape: a change list is a
// json array of {field,before,after}, and an omitted half stays omitted rather than
// arriving as null, which would be a claim that the old value was the json null.
func TestChangeMarshalsAsTheTrailStoresIt(t *testing.T) {
	got, err := events.Changes(nil, &row{Tagline: "x"}, "tagline")
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != `[{"field":"tagline","after":"x"}]` {
		t.Errorf("the payload is %s", body)
	}
}
