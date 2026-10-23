package translationtest

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/modules/translation/contracts"
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

// suggestTitle asks the machine for the title, which is the stub entity's one
// capped field and so the one field a provider's answer can be too long for.
func (f Fixture) suggestTitle(t *testing.T, id uuid.UUID) error {
	t.Helper()
	return f.Service.Suggest(f.Ctx, f.Tx, rest.SuggestQuery{
		Module: TestModule, Entity: TestEntity, Locale: PT, RecordID: id, Fields: []string{FieldTitle},
		Expected: expected(0, 0), Source: f.Source.Rows_[id].Values, From: EN,
	})
}

// overLong is a machine's answer longer than the stub entity's title ceiling and
// shorter than any other field's — which is the whole shape of the case below:
// the same text is too long for one field and a good draft for another.
const overLong = "Este título é demasiado longo para um título de página"

const (
	firstParagraph  = "We build software."
	secondParagraph = "Our office is in Lisbon."
	translatedBody  = "Construímos software.\n\nOur office is in Lisbon."
)

func cases() map[string]func(*testing.T, Fixture) {
	return map[string]func(*testing.T, Fixture){
		// C1: the overlay must fall back rather than serve "".
		"an empty store answers no fields": readEmptyTranslations,
		// C2: the locale is a key and not a filter.
		"a saved translation reads back in its own locale and says nothing": saveTranslation,
		// C3: idempotence, which is what lets a form be resubmitted.
		"the same text again writes nothing and says nothing": repeatTranslation,
		// C4: two translators, one loser, and no silent overwrite.
		"a stale expected revision is refused and the winner's text stands": refuseStaleRevision,
		// C7: the field's own rules run on a translation, and a refusal writes nothing.
		"an empty translation is refused and writes nothing": refuseEmptyTranslation,
		// C8, and C9 with it: the digest decides and nothing else does.
		"a source that moved makes the translation outdated, and one that did not does not": markChangedSource,
		// C10: the stale pair is the reviewer's evidence.
		"a second source edit leaves the source the translation was made from alone": preserveOriginalSource,
		// C11: journey 2's last step cannot be faked.
		"a stale translation cannot be marked reviewed, and a fixed one can": reviewUpdatedTranslation,
		// C12: the copy cannot drift from its own digest.
		"every stored row's digest is the digest of its own source copy": hashStoredSource,
		// C13 and C14, at the door they belong to.
		"a machine draft is withheld from the public door until a person reviews it": reviewMachineDraft,
		// C15: the event names the field, not merely that something happened.
		"every event names the field, the locale and the revision it moved": describeTranslationEvents,
		// C16: nothing dangles, and the record's delete is what removed it.
		"forgetting a record takes every locale of it": forgetAllLocales,
		// C18: the question no query over the translations table can answer.
		"the overview counts a record with no translation as missing": countMissingTranslations,
		// C23 and C24: an unavailable provider is not a saved draft, and a
		// draft is not a version history.
		"machine translation refuses when there is no provider and when the provider echoes": refuseUnavailableMachine,
		// A machine's answer is text nobody read, and the field it would be filed
		// under is the only thing that says how long it may be. Portuguese runs
		// longer than the English it came from, so the ordinary case for a capped
		// field is a draft that does not fit. It is refused under the rule the
		// person's own text is refused under, and the same text under a field with
		// no ceiling still saves — which is what says the ceiling refused it and
		// not the machine.
		"a machine draft longer than its field allows is refused and saves nothing": refuseOverlongDraft,
		// Untranslate is a write, so it is announced: the event is the only
		// record that a Portuguese existed at all.
		"removing a translation is a write and is announced": removeTranslation,
	}
}

func show(c rest.OverviewCounts) string {
	return fmt.Sprintf("missing %d, outdated %d, machine %d, complete %d",
		c.Missing, c.Outdated, c.Machine, c.Complete)
}
