package porttest

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// The generated suite against a fake that is right. It is the positive control
// and it is also the readable half of this file: the names below are the cases
// every port of this shape now owes its consumers.
func TestRunPassesAFakeThatIsRight(t *testing.T) {
	Run(t, noteSuite(knobs{}))
}

func TestNamesAreUnchanged(t *testing.T) {
	want := []string{
		"File: the operation says what it did",
		"File: the same command twice writes nothing and says nothing",
		"File: an unknown row is not found",
		"File: a caller with no grant is refused and writes nothing",
		"File: a revision the row is not at is refused and writes nothing",
		"File: another tenant cannot reach the row",
		"a sealed note is not filed twice",
		"the note carries the day it was filed",
	}
	got := Names(noteSuite(knobs{}))
	if len(got) != len(want) {
		t.Fatalf("the suite runs %d cases:\n%s\nwant %d:\n%s",
			len(got), strings.Join(got, "\n"), len(want), strings.Join(want, "\n"))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("case %d is %q, want %q; a case name is a requirement's evidence", i, got[i], want[i])
		}
	}
}

// The three mutation proofs: a broken fake, and the generated case that bites.

func TestRefusalCaseFailsWhenTheWriteLands(t *testing.T) {
	// The module's own case is left out here: it reports through the testing.T
	// it was handed, as a hand-written case does, and this test is about the
	// cases the harness generates.
	suite := noteSuite(knobs{writeFirst: true})
	suite.Own = nil
	log := watch(t, suite)
	log.mustFail(t, "File: a caller with no grant is refused and writes nothing", "a refused call wrote")
	log.mustFail(t, "a sealed note is not filed twice", "a refused call wrote")
}

func TestRetryCaseFailsWhenTheSecondCallEmits(t *testing.T) {
	log := watch(t, noteSuite(knobs{emitAlways: true}))
	log.mustFail(t, "File: the same command twice writes nothing and says nothing", "so it says nothing")
	log.mustPassApartFrom(t, "File: the same command twice writes nothing and says nothing")
}

func TestRetryCaseFailsWhenTheSecondCallAnswersARowNothingWrote(t *testing.T) {
	log := watch(t, noteSuite(knobs{answerMoves: true}))
	log.mustFail(t, "File: the same command twice writes nothing and says nothing", "a row nothing wrote")
	log.mustPassApartFrom(t, "File: the same command twice writes nothing and says nothing")
}

func TestTenantCaseFailsWhenTheStoreIsShared(t *testing.T) {
	log := watch(t, noteSuite(knobs{oneTenant: true}))
	log.mustFail(t, "File: another tenant cannot reach the row", "was not refused")
	log.mustPassApartFrom(t, "File: another tenant cannot reach the row")
}

// TestTenantCaseFailsWhenTheRefusalTookTheRowAwayFromItsOwner is the tenant
// case's own mutation proof, and the reason the store answers it rather than the
// description.
//
// The Snapshot below reads through the other tenant's context, which is what a
// description whose Provoke moves the world ends up doing: the refusal answered,
// the row is gone from the tenant that owns it, and either side of the refused
// call the snapshot renders the same nothing, because the visitor never saw the
// row to begin with. "A refused call wrote nothing" is then written from the far
// side of the wall, and only the store — which cannot be moved into another
// tenant's by a closure — can say the row it held is gone.
func TestTenantCaseFailsWhenTheRefusalTookTheRowAwayFromItsOwner(t *testing.T) {
	suite := noteSuite(knobs{erases: true})
	suite.Own = nil
	suite.Ops[0].Snapshot = func(t *testing.T, w world, row uuid.UUID) string {
		t.Helper()
		stored, err := w.service.Note(w.elsewhere, row)
		if err != nil {
			return "no such note"
		}
		return fmt.Sprintf("filed=%v sealed=%v rev=%d", stored.Filed, stored.Sealed, stored.Rev)
	}
	const name = "File: " + string(Elsewhere)
	log := watch(t, suite)
	log.mustFail(t, name, "still be there for the tenant that owns it")
	log.mustPassApartFrom(t, name)
}

// The floor: what Run refuses to run at all, before any case.

func TestRunRefusesAMutatingOpWithNoUnknownCaseAndNoReason(t *testing.T) {
	suite := noteSuite(knobs{})
	suite.Ops[0].Refusals = withoutKind(suite.Ops[0].Refusals, Unknown)
	watch(t, suite).mustRefuseTheSuite(t, "owes \"an unknown row is not found\" and neither describes it nor says why not")
}

func TestRunAcceptsASkipWithAReason(t *testing.T) {
	suite := noteSuite(knobs{})
	suite.Ops[0].Refusals = withoutKind(suite.Ops[0].Refusals, Unknown)
	suite.Ops[0].Skip = map[Kind]string{Unknown: "the id comes from the route, which kit/rest has already resolved"}
	log := watch(t, suite)
	log.mustPassApartFrom(t)
	if ran := log.names(); len(ran) != 7 {
		t.Errorf("the suite ran %d cases, want the seven that are left: %v", len(ran), ran)
	}
}

func TestRunRefusesASkipWithNoReason(t *testing.T) {
	suite := noteSuite(knobs{})
	suite.Ops[0].Refusals = withoutKind(suite.Ops[0].Refusals, Unknown)
	suite.Ops[0].Skip = map[Kind]string{Unknown: ""}
	watch(t, suite).mustRefuseTheSuite(t, "with no reason; the reason is the case")
}

func TestRunRefusesASkipForACaseThatIsDescribed(t *testing.T) {
	suite := noteSuite(knobs{})
	suite.Ops[0].Skip = map[Kind]string{Unknown: "the route resolves the id"}
	watch(t, suite).mustRefuseTheSuite(t, "is described and skipped")
}

func TestRunRefusesTwoOpsWithOneName(t *testing.T) {
	suite := noteSuite(knobs{})
	suite.Ops = append(suite.Ops, suite.Ops[0])
	watch(t, suite).mustRefuseTheSuite(t, "two operations named \"File\"")
}

func TestRunRefusesAnOwnCaseWithNoBecause(t *testing.T) {
	suite := noteSuite(knobs{})
	suite.Own[0].Because = ""
	watch(t, suite).mustRefuseTheSuite(t, "is hand-written and says no reason")
}

func TestRunRefusesAMutatingOpWithNoSnapshot(t *testing.T) {
	suite := noteSuite(knobs{})
	suite.Ops[0].Snapshot = nil
	watch(t, suite).mustRefuseTheSuite(t, "is unassertable without one")
}

func TestRunRefusesARefusalWithNoIs(t *testing.T) {
	suite := noteSuite(knobs{})
	suite.Ops[0].Refusals[0].Is = nil
	watch(t, suite).mustRefuseTheSuite(t, "what counts as that refusal is the port's own answer")
}

func TestRunRefusesAWorldThatHandsBackAZeroFixture(t *testing.T) {
	suite := noteSuite(knobs{})
	suite.World = func(_ *testing.T, run func(world)) { run(world{}) }
	log := watch(t, suite)
	log.mustFail(t, "File: the operation says what it did", "zero fixture")
}

// The plumbing a module's fake embeds.

func TestStorePanicsOutsideATenant(t *testing.T) {
	store := NewStore(func(n note) uuid.UUID { return n.ID })
	defer func() {
		if recovered := recover(); recovered == nil {
			t.Error("a store read outside a tenant answered; house rule 1 has no door that takes a tenant as an argument")
		}
	}()
	store.Get(context.Background(), uuid.New())
}

func TestStoreHandsOutCopies(t *testing.T) {
	ctx, store := oneTenantContext(), NewStore(func(n note) uuid.UUID { return n.ID })
	row := note{Title: "the minutes"}
	row.ID = uuid.New()
	store.Put(ctx, row)

	got, err := store.Get(ctx, row.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	got.Title = "somebody else's minutes"
	if got.Title != "somebody else's minutes" {
		t.Fatal("the row the store handed out could not be written to at all")
	}
	again, err := store.Get(ctx, row.ID)
	if err != nil {
		t.Fatalf("Get again: %v", err)
	}
	if again.Title != "the minutes" {
		t.Errorf("the store holds %q; a caller that mutates what it was handed cannot reach into the store", again.Title)
	}
}

func TestStoreListsInInsertionOrder(t *testing.T) {
	ctx, store := oneTenantContext(), NewStore(func(n note) uuid.UUID { return n.ID })
	var titles []string
	for _, title := range []string{"first", "second", "third", "fourth", "fifth"} {
		row := note{Title: title}
		row.ID = uuid.New()
		store.Put(ctx, row)
		titles = append(titles, title)
	}
	var got []string
	for _, row := range store.All(ctx) {
		got = append(got, row.Title)
	}
	if strings.Join(got, ",") != strings.Join(titles, ",") {
		t.Errorf("the store lists %v, want %v; a list case cannot pass by luck", got, titles)
	}
}

func TestStoreKeepsAnotherTenantsRowOutOfReach(t *testing.T) {
	store := NewStore(func(n note) uuid.UUID { return n.ID })
	here, there := oneTenantContext(), oneTenantContext()
	row := note{Title: "the minutes"}
	row.ID = uuid.New()
	store.Put(here, row)

	if _, err := store.Get(there, row.ID); !errors.Is(err, crud.ErrNotFound) {
		t.Errorf("another tenant's read answered %v, want ErrNotFound; a row somebody else holds is not found either", err)
	}
	if rows := store.All(there); len(rows) != 0 {
		t.Errorf("another tenant lists %d rows", len(rows))
	}
}

func TestSeedRefusesARowValidateRefuses(t *testing.T) {
	ctx := oneTenantContext()
	fake := NewFake(time.Now(), nil)
	store := NewStore(func(n note) uuid.UUID { return n.ID })
	defer func() {
		if recovered := recover(); recovered == nil {
			t.Error("a note with no title was seeded; a fixture that seeds a row the database would refuse tests the fake's tolerance")
		}
	}()
	Seed(ctx, fake, store, note{})
}

func TestRecorderRefusesAnUndeclaredEvent(t *testing.T) {
	fake := NewFake(time.Now(), []string{"note.filed"})
	if err := fake.Events.Emit("note.filed"); err != nil {
		t.Fatalf("a declared event: %v", err)
	}
	if err := fake.Events.Emit("note.shredded"); err == nil {
		t.Error("an undeclared event was recorded; an event a module never declared is a subscriber nobody wrote")
	}
	if got := fake.Events.Names(); len(got) != 1 || got[0] != "note.filed" {
		t.Errorf("the recorder holds %v, want the one declared event", got)
	}
	if fake.Events.Count() != 1 {
		t.Errorf("Count is %d, want 1", fake.Events.Count())
	}
}

func TestClockIsUTC(t *testing.T) {
	somewhere := time.FixedZone("UTC+7", 7*60*60)
	clock := NewClock(time.Date(2026, 9, 26, 9, 0, 0, 0, somewhere))
	if got := clock.Now(); got.Location() != time.UTC {
		t.Errorf("the clock reads %v in %v, want UTC", got, got.Location())
	}
	clock.Advance(90 * time.Minute)
	if got, want := clock.Now(), time.Date(2026, 9, 26, 3, 30, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("after an hour and a half the clock reads %v, want %v", got, want)
	}
	clock.Set(time.Date(2027, 1, 1, 0, 0, 0, 0, somewhere))
	if got := clock.Now(); got.Location() != time.UTC {
		t.Errorf("a clock set from another zone reads %v in %v", got, got.Location())
	}
}

func TestDoRefusesACallerWhoHoldsNothingBeforeItReads(t *testing.T) {
	ctx := oneTenantContext()
	fake := NewFake(time.Now(), []string{"note.filed"})
	store := NewStore(func(n note) uuid.UUID { return n.ID })
	// No row and no grant: the grant is the first thing a command checks, so
	// the answer is the denial and not "no such row".
	_, err := Do(ctx, fake, store, Command[note]{
		Permission: filePermission,
		Denied:     errDenied,
		Row:        uuid.New(),
		Apply:      func(*note) []string { return []string{"note.filed"} },
	})
	if !errors.Is(err, errDenied) {
		t.Errorf("Do answered %v, want the module's denial; a command that reads before it checks the grant tells a stranger the row exists", err)
	}
	if fake.Events.Count() != 0 {
		t.Error("a refused command published something")
	}
}

func TestDoRefusesAnUndeclaredEventWithoutWriting(t *testing.T) {
	ctx := oneTenantContext()
	fake := NewFake(time.Now(), []string{"note.filed"})
	store := NewStore(func(n note) uuid.UUID { return n.ID })
	row := Seed(ctx, fake, store, note{Title: "the minutes"})

	_, err := Do(ctx, fake, store, Command[note]{
		Row:   row,
		Apply: func(n *note) []string { n.Filed = true; return []string{"note.shredded"} },
	})
	if err == nil {
		t.Fatal("an undeclared event was published")
	}
	stored, err := store.Get(ctx, row)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if stored.Filed {
		t.Error("the row was written and the event was not; domain state and what the command says commit together")
	}
}

// The test reporter. Run reports through a narrow interface so this file can
// watch a generated case fail without failing the test that is watching.

type transcript struct {
	mu     sync.Mutex
	order  []string
	failed map[string][]string
}

type watcher struct {
	t    *testing.T
	name string
	log  *transcript
}

func watch(t *testing.T, s Suite[world]) *transcript {
	t.Helper()
	log := &transcript{failed: map[string][]string{}}
	run(watcher{t: t, log: log}, s)
	return log
}

func (w watcher) T() *testing.T { return w.t }
func (w watcher) Helper()       {}

func (w watcher) Errorf(format string, args ...any) {
	w.log.mu.Lock()
	defer w.log.mu.Unlock()
	w.log.failed[w.name] = append(w.log.failed[w.name], fmt.Sprintf(format, args...))
}

func (w watcher) Run(name string, f func(reporter)) bool {
	w.log.mu.Lock()
	w.log.order = append(w.log.order, name)
	w.log.mu.Unlock()
	f(watcher{t: w.t, name: name, log: w.log})
	return len(w.log.failures(name)) == 0
}

func (tr *transcript) failures(name string) []string {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	return tr.failed[name]
}

func (tr *transcript) names() []string {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	return tr.order
}

// mustFail is the assertion every mutation proof makes: this case failed, and it
// failed for the reason the harness is supposed to give.
func (tr *transcript) mustFail(t *testing.T, name, because string) {
	t.Helper()
	said := tr.failures(name)
	if len(said) == 0 {
		t.Fatalf("%q passed against a fake that is wrong; it ran %v", name, tr.names())
	}
	if !strings.Contains(strings.Join(said, "\n"), because) {
		t.Errorf("%q failed with %v, and none of it says %q", name, said, because)
	}
}

// mustPassApartFrom is the other half of a mutation proof: one broken thing
// fails one case, so a harness cannot pass this file by failing everything.
func (tr *transcript) mustPassApartFrom(t *testing.T, expected ...string) {
	t.Helper()
	for _, name := range tr.names() {
		if len(tr.failures(name)) == 0 {
			continue
		}
		if !slices.Contains(expected, name) {
			t.Errorf("%q also failed: %v", name, tr.failures(name))
		}
	}
	if suite := tr.failures(""); len(suite) > 0 {
		t.Errorf("the suite itself was refused: %v", suite)
	}
}

// mustRefuseTheSuite is the floor: the description was refused before a case
// ran, and the refusal said which correction to make.
func (tr *transcript) mustRefuseTheSuite(t *testing.T, because string) {
	t.Helper()
	said := tr.failures("")
	if len(said) == 0 {
		t.Fatalf("the suite ran: %v", tr.names())
	}
	if !strings.Contains(strings.Join(said, "\n"), because) {
		t.Errorf("the suite was refused with %v, and none of it says %q", said, because)
	}
	if ran := tr.names(); len(ran) > 0 {
		t.Errorf("%d cases ran under a description that was refused: %v", len(ran), ran)
	}
}

func withoutKind[W any](refusals []Refusal[W], kind Kind) []Refusal[W] {
	var out []Refusal[W]
	for _, r := range refusals {
		if r.Kind != kind {
			out = append(out, r)
		}
	}
	return out
}

func oneTenantContext() context.Context {
	return tenancy.WithTenant(context.Background(), tenancy.Tenant{ID: uuid.New(), Slug: "here"})
}

// TestRetryCaseFailsWhenTheAnswerRendersNothing is the same regression with a
// different shape. Deleting the comparison between the two answers is a diff
// somebody reads; emptying an operation's rendering is not, and it leaves the
// retry comparing two empty strings, which is green whatever the port answered.
// A mutating operation that renders nothing is refused out loud for that reason.
func TestRetryCaseFailsWhenTheAnswerRendersNothing(t *testing.T) {
	suite := noteSuite(knobs{})
	suite.Own = nil
	op := suite.Ops[0]
	op.Call = func(w world, row uuid.UUID) (string, error) {
		_, err := w.service.File(w.ctx, row, revisionOf(w, row))
		return "", err
	}
	suite.Ops[0] = op
	log := watch(t, suite)
	log.mustFail(t, "File: the same command twice writes nothing and says nothing", "two empty strings")
	log.mustPassApartFrom(t, "File: the same command twice writes nothing and says nothing")
}
