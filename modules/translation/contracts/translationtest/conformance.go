package translationtest

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/modules/translation/contracts"
	"github.com/septagon-oss/platformkit/modules/translation/internal"
)

// The clock the cases move the source forward by, spelled once.
var hour = 60 * time.Minute

// Fixture is one case's world: a Service, the transaction its commands take, the
// entity it translates, the optional machine, and a way to see what was
// published.
//
// Source is the case's own — Put a row on it and the entity has that row —
// because half of what this port is asked is a question about the *records*, and
// a suite with no records could not ask it.
type Fixture struct {
	Ctx     context.Context
	Tx      db.Tx[db.Tenant]
	Service contracts.Service
	Source  *StubSource
	Machine *Machine
	// Published is the event names emitted so far, in order. Every refusal in
	// the specification's table is asserted by a case that reads this before
	// and after: a refused mutation that publishes is a mutation.
	Published func() []string
	// Payloads is what those events carried.
	Payloads func() []contracts.Updated
	// Rows is every translation row of the transaction's tenant.
	Rows func() []Row
}

// Harness builds one Fixture and calls run with it. Written this way because the
// real service's fixture is a transaction, and a transaction is a scope somebody
// has to close.
type Harness func(t *testing.T, run func(Fixture))

// RunService is the conformance suite: every implementation of the port passes
// it, or it is not one.
func RunService(t *testing.T, h Harness) {
	t.Helper()
	for name, run := range cases() {
		t.Run(name, func(t *testing.T) {
			h(t, func(f Fixture) { run(t, f) })
		})
	}
}

// WithMachine gives the case a provider that answers fn.
func (f Fixture) WithMachine(fn func(text, from, to string) (string, error)) {
	f.Machine.Answer = func(_ context.Context, text, from, to string) (string, error) {
		return fn(text, from, to)
	}
}

// silent runs the step and fails if it published anything.
func (f Fixture) silent(t *testing.T, what string, step func()) {
	t.Helper()
	before := len(f.Published())
	step()
	if after := len(f.Published()); after != before {
		t.Errorf("%s published %d event(s) beyond the %d already out; a write that changes nothing has nothing to announce", what, after-before, before)
	}
}

// nTimes runs the step and fails unless it published exactly n events.
func (f Fixture) nTimes(t *testing.T, what string, n int, step func()) {
	t.Helper()
	before := len(f.Published())
	step()
	if after := len(f.Published()); after != before+n {
		t.Fatalf("%s published %d events, want exactly %d", what, after-before, n)
	}
}

// once runs the step and fails unless it published exactly one event.
func (f Fixture) once(t *testing.T, what string, step func()) {
	t.Helper()
	before := len(f.Published())
	step()
	if after := len(f.Published()); after != before+1 {
		t.Fatalf("%s published %d events, want exactly one", what, after-before)
	}
}

// page is the record every case starts from: two paragraphs of English and a
// title, registered as a live row of the entity.
func (f Fixture) page(title, first, second string) (uuid.UUID, map[string]string) {
	id := uuid.New()
	values := Source(title, first, second)
	f.Source.Put(id, db.Now(), values)
	return id, values
}

// save writes both translatable fields of one record in pt-PT.
func (f Fixture) save(id uuid.UUID, body, title string, expBody, expTitle int64) error {
	return f.Service.Save(f.Ctx, f.Tx, rest.SaveQuery{
		Module: TestModule, Entity: TestEntity, Locale: PT, RecordID: id,
		Values:   map[string]string{FieldBody: body, FieldTitle: title},
		Expected: expected(expBody, expTitle),
		Source:   f.Source.Rows_[id].Values, RichText: rich(),
	})
}

// read answers what one record looks like in pt-PT through one door.
func (f Fixture) read(t *testing.T, id uuid.UUID, public bool) map[string]rest.TranslatedField {
	t.Helper()
	recs, err := f.Service.Translated(f.Ctx, f.Tx, rest.TranslatedQuery{
		Module: TestModule, Entity: TestEntity, Locale: PT, RecordIDs: []uuid.UUID{id},
		Sources:  map[uuid.UUID]map[string]string{id: f.Source.Rows_[id].Values},
		RichText: rich(), Public: public,
	})
	if err != nil {
		t.Fatalf("Translated: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("Translated answered %d records for one asked", len(recs))
	}
	return recs[0].Fields
}

// review marks the body reviewed, which is the one action journey 2 ends with.
func (f Fixture) review(t *testing.T, id uuid.UUID, fields ...string) error {
	t.Helper()
	if len(fields) == 0 {
		fields = []string{FieldBody}
	}
	return f.Service.Review(f.Ctx, f.Tx, rest.ReviewQuery{
		Module: TestModule, Entity: TestEntity, Locale: PT, RecordID: id, Fields: fields,
		Source: f.Source.Rows_[id].Values, RichText: rich(),
	})
}

// suggest asks the machine for the body at the revision the caller read.
func (f Fixture) suggest(t *testing.T, id uuid.UUID) error {
	t.Helper()
	return f.suggestAt(t, id, 0)
}

func (f Fixture) suggestAt(t *testing.T, id uuid.UUID, revision int64) error {
	t.Helper()
	return f.Service.Suggest(f.Ctx, f.Tx, rest.SuggestQuery{
		Module: TestModule, Entity: TestEntity, Locale: PT, RecordID: id, Fields: []string{FieldBody},
		Expected: expected(revision, revision), Source: f.Source.Rows_[id].Values, From: EN,
	})
}

const (
	firstParagraph  = "We build software."
	secondParagraph = "Our office is in Lisbon."
	translatedBody  = "Construímos software.\n\nOur office is in Lisbon."
)

func cases() map[string]func(*testing.T, Fixture) {
	return map[string]func(*testing.T, Fixture){
		// C1: the overlay must fall back rather than serve "".
		"an empty store answers no fields": func(t *testing.T, f Fixture) {
			id, _ := f.page("About us", firstParagraph, secondParagraph)
			if fields := f.read(t, id, false); len(fields) != 0 {
				t.Errorf("Translated answered %d fields of a record with no translation", len(fields))
			}
		},

		// C2: the locale is a key and not a filter.
		"a saved translation reads back in its own locale and says nothing": func(t *testing.T, f Fixture) {
			id, _ := f.page("About us", firstParagraph, secondParagraph)
			if err := f.save(id, translatedBody, "Sobre nós", 0, 0); err != nil {
				t.Fatalf("Save: %v", err)
			}
			fields := f.read(t, id, false)
			if fields[FieldBody].Value != translatedBody {
				t.Errorf("body is %q, want the Portuguese that was written", fields[FieldBody].Value)
			}
			if fields[FieldTitle].Value != "Sobre nós" {
				t.Errorf("title is %q", fields[FieldTitle].Value)
			}
			if fields[FieldBody].Status != "" || fields[FieldTitle].Status != "" {
				t.Errorf("a complete translation reports %q and %q; a field with nothing wrong with it is absent, not labelled complete",
					fields[FieldBody].Status, fields[FieldTitle].Status)
			}
			for _, row := range f.Rows() {
				if row.Locale != PT {
					t.Errorf("a row landed in %q, which nobody asked to write", row.Locale)
				}
			}
		},

		// C3: idempotence, which is what lets a form be resubmitted.
		"the same text again writes nothing and says nothing": func(t *testing.T, f Fixture) {
			id, _ := f.page("About us", firstParagraph, secondParagraph)
			f.nTimes(t, "the first save", 2, func() {
				if err := f.save(id, translatedBody, "Sobre nós", 0, 0); err != nil {
					t.Fatalf("Save: %v", err)
				}
			})
			f.silent(t, "the second save", func() {
				if err := f.save(id, translatedBody, "Sobre nós", 1, 1); err != nil {
					t.Fatalf("Save of the same text: %v", err)
				}
			})
			for _, row := range f.Rows() {
				if row.Revision != 1 {
					t.Errorf("%s sits at revision %d after a save that changed nothing; a revision a form cannot trust is worse than none",
						row.Field, row.Revision)
				}
			}
		},

		// C4: two translators, one loser, and no silent overwrite.
		"a stale expected revision is refused and the winner's text stands": func(t *testing.T, f Fixture) {
			id, _ := f.page("About us", firstParagraph, secondParagraph)
			if err := f.save(id, "Primeira.", "Primeiro", 0, 0); err != nil {
				t.Fatalf("the first translator's save: %v", err)
			}
			f.silent(t, "the refused save", func() {
				err := f.save(id, "Perdedora.", "Perdedor", 99, 99)
				if !errors.Is(err, crud.ErrConflict) {
					t.Fatalf("a stale revision answered %v, want crud.ErrConflict", err)
				}
			})
			if got := f.read(t, id, false)[FieldBody].Value; got != "Primeira." {
				t.Errorf("the loser overwrote the winner: body is %q", got)
			}
		},

		// C7: the field's own rules run on a translation, and a refusal writes nothing.
		"an empty translation is refused and writes nothing": func(t *testing.T, f Fixture) {
			id, _ := f.page("About us", firstParagraph, secondParagraph)
			f.silent(t, "the blank save", func() {
				err := f.save(id, "   ", "Sobre nós", 0, 0)
				if !errors.Is(err, crud.ErrInvalid) {
					t.Fatalf("a blank body answered %v, want crud.ErrInvalid", err)
				}
			})
			if rows := f.Rows(); len(rows) != 0 {
				t.Errorf("a refused save left %d rows behind", len(rows))
			}
		},

		// C8, and C9 with it: the digest decides and nothing else does.
		"a source that moved makes the translation outdated, and one that did not does not": func(t *testing.T, f Fixture) {
			id, _ := f.page("About us", firstParagraph, secondParagraph)
			if err := f.save(id, translatedBody, "Sobre nós", 0, 0); err != nil {
				t.Fatalf("Save: %v", err)
			}
			f.Source.Put(id, db.Now().Add(hour), Source("About us", firstParagraph, secondParagraph))
			if got := f.read(t, id, false)[FieldBody].Status; got != "" {
				t.Errorf("a source saved with the same words reports %q; the digest decides and the digest did not move", got)
			}
			f.Source.Put(id, db.Now().Add(hour), Source("About us", "We build software for banks.", secondParagraph))
			fields := f.read(t, id, false)
			if fields[FieldBody].Status != rest.FallbackOutdated {
				t.Errorf("body reports %q after its source changed, want %q", fields[FieldBody].Status, rest.FallbackOutdated)
			}
			if !strings.HasPrefix(fields[FieldBody].Value, "Constru") {
				t.Errorf("an outdated translation stopped being served: %q — stale is labelled, not hidden", fields[FieldBody].Value)
			}
		},

		// C10: the stale pair is the reviewer's evidence.
		"a second source edit leaves the source the translation was made from alone": func(t *testing.T, f Fixture) {
			id, _ := f.page("About us", firstParagraph, secondParagraph)
			if err := f.save(id, translatedBody, "Sobre nós", 0, 0); err != nil {
				t.Fatalf("Save: %v", err)
			}
			for _, moved := range []string{"We build software for banks.", "We build software for banks and insurers."} {
				f.Source.Put(id, db.Now().Add(hour), Source("About us", moved, secondParagraph))
			}
			var stored *Row
			rows := f.Rows()
			for i := range rows {
				if rows[i].Field == FieldBody {
					stored = &rows[i]
				}
			}
			if stored == nil {
				t.Fatal("the body's translation is gone")
			}
			if !strings.Contains(stored.SourceText, firstParagraph) || strings.Contains(stored.SourceText, "banks") {
				t.Errorf("the source copy was re-based onto the newest source (%q); the paragraph the reviewer was told to look at is gone with it",
					stored.SourceText)
			}
		},

		// C11: journey 2's last step cannot be faked.
		"a stale translation cannot be marked reviewed, and a fixed one can": func(t *testing.T, f Fixture) {
			id, _ := f.page("About us", firstParagraph, secondParagraph)
			if err := f.save(id, translatedBody, "Sobre nós", 0, 0); err != nil {
				t.Fatalf("Save: %v", err)
			}
			f.Source.Put(id, db.Now().Add(hour), Source("About us", "We build software for banks.", secondParagraph))
			f.silent(t, "the review of a stale translation", func() {
				if err := f.review(t, id); !errors.Is(err, crud.ErrInvalid) {
					t.Fatalf("reviewing a stale translation answered %v, want a refusal naming the source", err)
				}
			})
			if err := f.save(id, "Construímos software para bancos.\n\n"+secondParagraph, "Sobre nós", 1, 1); err != nil {
				t.Fatalf("the corrected save: %v", err)
			}
			// The corrected save is a person's own text, and a person's text is
			// reviewed by being typed: the review that follows succeeds and
			// writes nothing, because the only thing a review can record is that
			// somebody accepted a machine draft.
			f.silent(t, "the review of a human translation", func() {
				if err := f.review(t, id); err != nil {
					t.Fatalf("Review after the fix: %v", err)
				}
			})
			if got := f.read(t, id, false)[FieldBody].Status; got != "" {
				t.Errorf("after a fix the body still reports %q", got)
			}
		},

		// C12: the copy cannot drift from its own digest.
		"every stored row's digest is the digest of its own source copy": func(t *testing.T, f Fixture) {
			id, _ := f.page("About us", firstParagraph, secondParagraph)
			if err := f.save(id, "Construímos software.", "Sobre nós", 0, 0); err != nil {
				t.Fatalf("Save: %v", err)
			}
			if err := f.save(id, "Também.", "Também", 1, 1); err != nil {
				t.Fatalf("the second save: %v", err)
			}
			rows := f.Rows()
			if len(rows) != 2 {
				t.Fatalf("the store holds %d rows, want the two fields", len(rows))
			}
			for _, r := range rows {
				hash, err := internal.Hash(r.SourceText, r.Field == FieldBody)
				if err != nil {
					t.Fatalf("hashing %s: %v", r.Field, err)
				}
				if hash != r.SourceHash {
					t.Errorf("%s stores a source_hash that is not the digest of its own source_text", r.Field)
				}
			}
		},

		// C13 and C14, at the door they belong to.
		"a machine draft is withheld from the public door until a person reviews it": func(t *testing.T, f Fixture) {
			id, _ := f.page("About us", firstParagraph, secondParagraph)
			f.WithMachine(func(text, from, to string) (string, error) { return "Máquina: " + text, nil })
			f.once(t, "the suggestion", func() {
				if err := f.suggest(t, id); err != nil {
					t.Fatalf("Suggest: %v", err)
				}
			})
			workspace := f.read(t, id, false)
			if workspace[FieldBody].Status != rest.FallbackMachine {
				t.Errorf("the workspace sees %q, where a labelled draft belongs", workspace[FieldBody].Status)
			}
			public := f.read(t, id, true)
			if public[FieldBody].Status != rest.FallbackWithheld {
				t.Errorf("the public door sees %q, want %q", public[FieldBody].Status, rest.FallbackWithheld)
			}
			if strings.HasPrefix(public[FieldBody].Value, "Máquina") {
				t.Errorf("the public door served the machine draft: %q", public[FieldBody].Value)
			}
			f.once(t, "the review", func() {
				if err := f.review(t, id); err != nil {
					t.Fatalf("Review: %v", err)
				}
			})
			after := f.read(t, id, true)
			if after[FieldBody].Status != "" || !strings.HasPrefix(after[FieldBody].Value, "Máquina") {
				t.Errorf("after review the public door reports %q / %q, want the text and nothing to say",
					after[FieldBody].Status, after[FieldBody].Value)
			}
		},

		// C15: the event names the field, not merely that something happened.
		"every event names the field, the locale and the revision it moved": func(t *testing.T, f Fixture) {
			id, _ := f.page("About us", firstParagraph, secondParagraph)
			f.nTimes(t, "the save", 2, func() {
				if err := f.save(id, "Sobre nós.", "Sobre nós", 0, 0); err != nil {
					t.Fatalf("Save: %v", err)
				}
			})
			events := f.Payloads()
			if len(events) != 2 {
				t.Fatalf("a save of two fields published %d events, want one per field", len(events))
			}
			seen := map[string]bool{}
			for _, ev := range events {
				if ev.Module != TestModule || ev.Entity != TestEntity || ev.Locale != PT || ev.RecordID != id {
					t.Errorf("an event names %s.%s/%s of %s, want this record", ev.Module, ev.Entity, ev.Locale, ev.RecordID)
				}
				if ev.Revision != 1 || ev.Origin != rest.OriginHuman || ev.SourceHash == "" {
					t.Errorf("%s was announced at revision %d from %q with hash %q", ev.Field, ev.Revision, ev.Origin, ev.SourceHash)
				}
				seen[ev.Field] = true
			}
			if !seen[FieldBody] || !seen[FieldTitle] {
				t.Errorf("the fields announced were %v", sortedKeys(seen))
			}
		},

		// C16: nothing dangles, and the record's delete is what removed it.
		"forgetting a record takes every locale of it": func(t *testing.T, f Fixture) {
			id, _ := f.page("About us", firstParagraph, secondParagraph)
			if err := f.save(id, "Sobre nós.", "Sobre nós", 0, 0); err != nil {
				t.Fatalf("Save: %v", err)
			}
			// Two events: the record had both fields translated, and each is a
			// field that stopped existing. One event for the pair would leave a
			// subscriber unable to say which text went away.
			f.nTimes(t, "forgetting the record", 2, func() {
				if err := f.Service.ForgetRecord(f.Ctx, f.Tx, TestModule, TestEntity, id); err != nil {
					t.Fatalf("ForgetRecord: %v", err)
				}
			})
			if rows := f.Rows(); len(rows) != 0 {
				t.Errorf("the record is gone and %d of its translations remain", len(rows))
			}
			if fields := f.read(t, id, false); len(fields) != 0 {
				t.Errorf("a forgotten record still answers %d translated fields", len(fields))
			}
		},

		// C18: the question no query over the translations table can answer.
		"the overview counts a record with no translation as missing": func(t *testing.T, f Fixture) {
			translated, _ := f.page("About us", firstParagraph, secondParagraph)
			untouched, _ := f.page("Careers", "We are hiring.", "")
			if err := f.save(translated, translatedBody, "Sobre nós", 0, 0); err != nil {
				t.Fatalf("Save: %v", err)
			}
			rows, counts, total, err := f.Service.Overview(f.Ctx, f.Tx, rest.OverviewQuery{
				Module: TestModule, Entity: TestEntity, Locale: PT, Limit: 10,
			})
			if err != nil {
				t.Fatalf("Overview: %v", err)
			}
			if total != 2 || len(rows) != 2 {
				t.Fatalf("the overview paged %d of %d records, want both", len(rows), total)
			}
			// The counts are per field, which is what the overview's columns
			// are: two fields × two records, one record complete and one
			// untouched.
			if counts.Missing != 2 || counts.Complete != 2 || counts.Outdated != 0 {
				t.Errorf("the counts are %s, want 2 missing and 2 complete", show(counts))
			}
			for _, row := range rows {
				if row.ID != untouched {
					continue
				}
				if row.Missing != 2 || row.Complete != 0 {
					t.Errorf("the untranslated record reports %d missing and %d complete; a record with no row is missing, and only the entity's own row set can say so",
						row.Missing, row.Complete)
				}
			}
		},

		// C23 and C24: an unavailable provider is not a saved draft, and a
		// draft is not a version history.
		"machine translation refuses when there is no provider and when the provider echoes": func(t *testing.T, f Fixture) {
			id, _ := f.page("About us", firstParagraph, secondParagraph)
			f.silent(t, "a suggestion with no provider", func() {
				if err := f.suggest(t, id); !errors.Is(err, crud.ErrInvalid) {
					t.Fatalf("Suggest with no translator answered %v, want the refusal that names the setting", err)
				}
			})
			if rows := f.Rows(); len(rows) != 0 {
				t.Errorf("a refusal wrote %d rows", len(rows))
			}

			f.WithMachine(func(text, from, to string) (string, error) { return "Rascunho: " + text, nil })
			if err := f.suggest(t, id); err != nil {
				t.Fatalf("the first Suggest: %v", err)
			}
			f.WithMachine(func(text, from, to string) (string, error) { return text, nil })
			f.silent(t, "the echoing suggestion", func() {
				if err := f.suggest(t, id); err == nil {
					t.Fatal("a provider that handed back the English was accepted as Portuguese")
				}
			})
			f.WithMachine(func(text, from, to string) (string, error) { return "Segundo: " + text, nil })
			if err := f.suggestAt(t, id, 1); err != nil {
				t.Fatalf("the second Suggest: %v", err)
			}
			var revisions []int64
			for _, r := range f.Rows() {
				if r.Field == FieldBody {
					revisions = append(revisions, r.Revision)
				}
			}
			if len(revisions) != 1 || revisions[0] != 2 {
				t.Errorf("two drafts of one field left revisions %v, want one row at revision 2", revisions)
			}
			if got := f.read(t, id, false)[FieldBody].Status; got != rest.FallbackMachine {
				t.Errorf("the second draft reports %q; a draft is unreviewed until somebody says otherwise", got)
			}
		},

		// Untranslate is a write, so it is announced: the event is the only
		// record that a Portuguese existed at all.
		"removing a translation is a write and is announced": func(t *testing.T, f Fixture) {
			id, _ := f.page("About us", firstParagraph, secondParagraph)
			if err := f.save(id, "Sobre nós.", "Sobre nós", 0, 0); err != nil {
				t.Fatalf("Save: %v", err)
			}
			f.once(t, "untranslating the body", func() {
				if err := f.Service.Untranslate(f.Ctx, f.Tx, rest.ReviewQuery{
					Module: TestModule, Entity: TestEntity, Locale: PT, RecordID: id, Fields: []string{FieldBody},
				}); err != nil {
					t.Fatalf("Untranslate: %v", err)
				}
			})
			events := f.Payloads()
			last := events[len(events)-1]
			if last.Field != FieldBody || last.Status != rest.FallbackRemoved {
				t.Errorf("the removal was announced as %s/%s, want %s removed", last.Field, last.Status, FieldBody)
			}
			if fields := f.read(t, id, false); len(fields) != 1 || fields[FieldTitle].Value == "" {
				t.Errorf("after removing the body, %d fields remain: %v", len(fields), fields)
			}
		},
	}
}

func show(c rest.OverviewCounts) string {
	return fmt.Sprintf("missing %d, outdated %d, machine %d, complete %d",
		c.Missing, c.Outdated, c.Machine, c.Complete)
}
