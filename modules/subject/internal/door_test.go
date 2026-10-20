package internal_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/personaldata"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/subject"
	"github.com/septagon-oss/platformkit/modules/subject/contracts"
	"github.com/septagon-oss/platformkit/modules/subject/internal"
	"github.com/septagon-oss/platformkit/modules/user"
)

// The door, end to end, over the module's own two tables.
//
// What these cases hold fixed is the sequence ADR 0020 §5 and §6 decide: a request that
// commits together with the event that queues it; a walk that produces the bundle, stores
// it, holds it and tells the person; a link that opens once; and no answer that distinguishes
// an expired link from a forged one. What they cannot prove is the delivery — that the bytes
// landed in a real object store under a real retention hold, and that a person got a real
// notice — because the stubs below stand in for the file and notification modules. That line
// is drawn in apps/platformkit, the only place the three are composed together.

// svcLinkExpiry is the instant the mint said its link dies at, for the case that asks the
// request row the same question back.
func svcLinkExpiry(l *contracts.Link) time.Time { return l.ExpiresAt }

// stamps is the clock these cases wind: frozen at one instant, and moved forward by a case
// that needs to stand in the future to see whether a link has died.
type stamps struct{ at time.Time }

func (d *stamps) Now() time.Time { return d.at }

// bundlesStub is where a bundle goes: it remembers the bytes it was handed and the instant
// until which it was asked to keep them.
type bundlesStub struct {
	puts      int
	holds     int
	discards  int
	discarded uuid.UUID
	name      string
	body      []byte
	until     time.Time
	id        uuid.UUID
	err       error
}

func (b *bundlesStub) Put(_ context.Context, _ contracts.Tx, name string, body []byte) (uuid.UUID, error) {
	if b.err != nil {
		return uuid.Nil, b.err
	}
	b.puts++
	b.name, b.body = name, body
	if b.id == uuid.Nil {
		b.id = uuid.New()
	}
	return b.id, nil
}

// Hold records the instant the bundle was last asked to survive to — the assertion the
// mint case makes about it is the point of having the method at all.
func (b *bundlesStub) Hold(_ context.Context, _ db.Tx[db.Tenant], _ uuid.UUID, until time.Time) error {
	b.holds++
	b.until = until
	return nil
}

// Discard counts the bundles the erasure asked the store to end. The claim the case below
// makes about it is that an erasure reaches the copy this module made of a person, not only
// the rows that named them.
func (b *bundlesStub) Discard(_ context.Context, _ db.Tx[db.Tenant], file uuid.UUID) error {
	b.discards++
	b.discarded = file
	return nil
}

// notesStub is who the person is told. It keeps what it was asked to say, because the claim
// worth checking is that the notice points at the request and carries no token.
type notesStub struct{ got []contracts.Notice }

func (n *notesStub) Ready(_ context.Context, _ db.Tx[db.Tenant], notice contracts.Notice) error {
	n.got = append(n.got, notice)
	return nil
}

// doorSvc is what these cases need of the service: the door's commands, and the one method
// only the worker calls. Both come out of the one value the module builds, which is the
// point — the routes, the commands and the queue are served by one value, not three.
type doorSvc interface {
	contracts.Service
	internal.Walker
}

// newDoor builds that value over the stubs. `clock` is the time, so a case that needs the
// future can wind it; `asked` records which owners were asked, and one owner is enough for
// the walk's shape, which is what is under test here rather than the owners' work.
func newDoor(clock *stamps, bundles contracts.BundleSource, notes contracts.Notifier, asked ...*recorder) doorSvc {
	rec := &recorder{}
	if len(asked) > 0 && asked[0] != nil {
		rec = asked[0]
	}
	svc := internal.NewService(internal.Deps{
		Parts:   []personaldata.Part{{Module: "alpha", Data: ownerStub{name: "alpha", rec: rec}}},
		People:  peopleStub{},
		Bundles: bundles,
		Notes:   notes,
		Now:     clock.Now,
	})
	d, ok := svc.(doorSvc)
	if !ok {
		panic("subject: the service the module builds is the one the walk runs; it would have to be here")
	}
	return d
}

// walk runs one tenant's tick the way the scheduler does: a context carrying the tenant and
// no open transaction.
func walk(t *testing.T, svc doorSvc, conn *db.Conn, tenant tenancy.Tenant) error {
	t.Helper()
	return svc.WalkTenant(tenancy.WithTenant(t.Context(), tenant), conn)
}

// act is transact with an actor on the context: the door records who asked, so every case
// that stands at one has to say who is standing there.
func act(t *testing.T, conn *db.Conn, actor uuid.UUID, fn func(context.Context, db.Tx[db.Tenant]) error) error {
	t.Helper()
	return db.Run(tenancy.WithActor(tenancy.WithTenant(t.Context(), acme), actor), conn, fn)
}

// readRequest reads one request row back and hands it to the case that asked for it.
func readRequest(t *testing.T, conn *db.Conn, id uuid.UUID) *contracts.Request {
	t.Helper()
	row := &contracts.Request{}
	err := transact(t, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().WithContext(ctx).Where("id = ?", id).First(row).Error
	})
	if err != nil {
		t.Fatalf("read the request back: %v", err)
	}
	return row
}

// ask is one person, filing one request, in its own transaction — the three cases that
// start this way are the whole of the repetition it saves.
func ask(t *testing.T, svc doorSvc, conn *db.Conn, person personaldata.Subject) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := act(t, conn, person.ID, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		row, err := svc.RequestExport(ctx, tx, contracts.RequestExportInput{Subject: person.ID})
		if err != nil {
			return err
		}
		id = row.ID
		return err
	})
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	return id
}

func TestARequestCommitsWithTheEventThatQueuesIt(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, subject.Migrations)
	svc := newDoor(&stamps{at: time.Date(2026, time.October, 9, 12, 0, 0, 0, time.UTC)}, &bundlesStub{}, &notesStub{})
	person := personaldata.Subject{ID: uuid.New(), Email: "requester@example.com", DisplayName: "Rhea", Handle: "rhea"}
	seed(t, conn, person)
	id := ask(t, svc, conn, person)

	row := readRequest(t, conn, id)
	if row.State != contracts.StateRequested || row.Kind != contracts.KindExport {
		t.Errorf("request = %+v, want a requested export", row)
	}
	if row.Requester != person.ID {
		t.Errorf("the request names %s as its requester, not the person who asked (%s)", row.Requester, person.ID)
	}
	if n := published(t, conn, contracts.EventExportRequested); n != 1 {
		t.Errorf("%d %s events: the row and the event commit together or the queue has a hole in it", n, contracts.EventExportRequested)
	}

	// The same row, asked back by the person who filed it, and by somebody who did not.
	if err := act(t, conn, person.ID, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		got, err := svc.Request(ctx, tx, id, person.ID, false)
		if err != nil {
			return err
		}
		if got.ID != id || got.State != contracts.StateRequested {
			t.Errorf("read back %+v", got)
		}
		return nil
	}); err != nil {
		t.Errorf("the person could not read their own request: %v", err)
	}
	if err := act(t, conn, uuid.New(), func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := svc.Request(ctx, tx, id, uuid.New(), false)
		return err
	}); !errors.Is(err, crud.ErrNotFound) {
		t.Errorf("somebody else reading the request: got %v, want %v", err, crud.ErrNotFound)
	}

	// A reason past the ceiling is refused before the row, because a row that outlives the
	// conversation the sentence was written for is a record nobody can read.
	err := act(t, conn, person.ID, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := svc.RequestExport(ctx, tx, contracts.RequestExportInput{
			Subject: person.ID, Reason: strings.Repeat("x", contracts.MaxReason+1),
		})
		return err
	})
	if !errors.Is(err, crud.ErrInvalid) {
		t.Errorf("a reason past the ceiling: got %v, want %v", err, crud.ErrInvalid)
	}
	if n := published(t, conn, contracts.EventExportRequested); n != 1 {
		t.Errorf("%d events after a refused request: a refusal queues nothing", n)
	}
}

func TestTheWalkProducesTheBundleStoresItAndTellsThePerson(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, subject.Migrations)
	at := time.Date(2026, time.October, 9, 12, 0, 0, 0, time.UTC)
	stubbed, notes := &bundlesStub{}, &notesStub{}
	svc := newDoor(&stamps{at: at}, stubbed, notes)
	person := personaldata.Subject{ID: uuid.New(), Email: "walker@example.com", DisplayName: "Wren", Handle: "wren"}
	seed(t, conn, person)
	id := ask(t, svc, conn, person)

	if err := walk(t, svc, conn, acme); err != nil {
		t.Fatalf("walk: %v", err)
	}

	row := readRequest(t, conn, id)
	if row.State != contracts.StateReady {
		t.Errorf("state = %q, want ready", row.State)
	}
	if row.Bundle != stubbed.id {
		t.Errorf("bundle = %s, want the file the store answered with (%s)", row.Bundle, stubbed.id)
	}
	if row.BundleDigest == "" || len(stubbed.body) == 0 {
		t.Errorf("a ready request with no digest (%q) or no bytes (%d)", row.BundleDigest, len(stubbed.body))
	}
	if row.LinkExpires != nil {
		t.Errorf("link expires = %v before any link was minted", row.LinkExpires)
	}
	if stubbed.holds != 1 || !stubbed.until.Equal(at.Add(contracts.LinkLife)) {
		t.Errorf("the bundle is held to %v after %d holds, want the walk's own seven days (%s)",
			stubbed.until, stubbed.holds, at.Add(contracts.LinkLife))
	}
	if !strings.HasPrefix(stubbed.name, "export-"+id.String()) {
		t.Errorf("bundle name = %q, want it to name the request it answers", stubbed.name)
	}
	if len(notes.got) != 1 || notes.got[0].Recipient != person.ID {
		t.Fatalf("notices = %+v, want one to the person who asked", notes.got)
	}
	if strings.Contains(notes.got[0].Link, "token") || strings.Contains(notes.got[0].Link, id.String()+"?") {
		t.Errorf("the notice's link = %q: a notice points at the request, never at a credential", notes.got[0].Link)
	}
	if n := published(t, conn, contracts.EventExportReady); n != 1 {
		t.Errorf("%d %s events published", n, contracts.EventExportReady)
	}

	// A second walk finds nothing to do: a `ready` row is not re-bundled, because the
	// second bundle would replace the digest the person was told about.
	if err := walk(t, svc, conn, acme); err != nil {
		t.Fatalf("second walk: %v", err)
	}
	if stubbed.puts != 1 {
		t.Errorf("a second walk stored %d bundles, want 0 more", stubbed.puts)
	}
}

func TestAFailingWalkFilesTheFailureItRan(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, subject.Migrations)
	stubbed := &bundlesStub{err: errors.New("the store is not answering")}
	svc := newDoor(&stamps{at: time.Now().UTC()}, stubbed, &notesStub{})
	person := personaldata.Subject{ID: uuid.New(), Email: "failed@example.com", DisplayName: "Faye", Handle: "faye"}
	seed(t, conn, person)
	id := ask(t, svc, conn, person)

	if err := walk(t, svc, conn, acme); err == nil {
		t.Fatal("a walk whose store refused reported no failure")
	}
	row := readRequest(t, conn, id)
	if row.State != contracts.StateFailed {
		t.Errorf("state = %q, want failed", row.State)
	}
	if !strings.Contains(row.Detail, "not answering") {
		t.Errorf("detail = %q: the row has to say which call said no", row.Detail)
	}
	if row.Bundle != uuid.Nil {
		t.Errorf("a failed request names a bundle: %s", row.Bundle)
	}
	if n := published(t, conn, contracts.EventExportReady); n != 0 {
		t.Errorf("%d %s events from a walk that stored nothing", n, contracts.EventExportReady)
	}
}

func TestTheLinkOpensOnceAndTheRowHoldsNoToken(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, subject.Migrations)
	svc := newDoor(&stamps{at: time.Date(2026, time.October, 9, 12, 0, 0, 0, time.UTC)}, &bundlesStub{}, &notesStub{})
	person := personaldata.Subject{ID: uuid.New(), Email: "linker@example.com", DisplayName: "Lena", Handle: "lena"}
	seed(t, conn, person)
	id := ask(t, svc, conn, person)
	if err := walk(t, svc, conn, acme); err != nil {
		t.Fatalf("walk: %v", err)
	}

	var link *contracts.Link
	if err := act(t, conn, person.ID, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		link, err = svc.Mint(ctx, tx, id, person.ID, false)
		return err
	}); err != nil {
		t.Fatalf("mint: %v", err)
	}

	// Somebody else's request answers not-found, which is the answer that does not confirm
	// the row exists.
	if err := act(t, conn, uuid.New(), func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := svc.Mint(ctx, tx, id, uuid.New(), false)
		return err
	}); !errors.Is(err, crud.ErrNotFound) {
		t.Errorf("a stranger minting: got %v, want %v", err, crud.ErrNotFound)
	}

	// The table holds a hash of the token, never the token: a read of this table cannot
	// hand out a working link, which is the whole reason it is stored this way.
	var stored string
	if err := transact(t, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw(`SELECT token_hash FROM subject_grants WHERE request_id = ?`, id).Row().Scan(&stored)
	}); err != nil {
		t.Fatalf("read the grant row: %v", err)
	}
	if stored == link.Token {
		t.Fatal("the grant row carries the token itself")
	}
	if stored != contracts.HashToken(link.Token) {
		t.Errorf("stored hash = %q, want the hash of the token that was handed out", stored)
	}
	// Minting the link moved the end of the hold out to the end of the link's life: a
	// bundle held only to the walk's own instant would be swept under a person who asked
	// for their door a week later.
	if row := readRequest(t, conn, id); row.LinkExpires == nil || !row.LinkExpires.Equal(svcLinkExpiry(link)) {
		t.Errorf("link expires = %v, want %s", row.LinkExpires, link.ExpiresAt)
	}

	if err := transact(t, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		got, err := svc.Redeem(ctx, tx, link.Token)
		if err != nil {
			return err
		}
		if got.File != link.File || got.Request != id {
			t.Errorf("redeemed %+v, want the bundle the link points at", got)
		}
		return nil
	}); err != nil {
		t.Fatalf("redeem: %v", err)
	}
	// Used, forged and — in the case below — too old are one answer.
	if err := transact(t, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := svc.Redeem(ctx, tx, link.Token)
		return err
	}); !errors.Is(err, contracts.ErrGrant) {
		t.Errorf("a link opened twice: got %v, want %v", err, contracts.ErrGrant)
	}
	if err := transact(t, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := svc.Redeem(ctx, tx, strings.Repeat("0", 64))
		return err
	}); !errors.Is(err, contracts.ErrGrant) {
		t.Errorf("a forged token: got %v, want %v", err, contracts.ErrGrant)
	}
}

func TestAnExpiredLinkAnswersTheRefusalAForgedOneGets(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, subject.Migrations)
	clock := &stamps{at: time.Date(2026, time.October, 9, 12, 0, 0, 0, time.UTC)}
	svc := newDoor(clock, &bundlesStub{}, &notesStub{})
	person := personaldata.Subject{ID: uuid.New(), Email: "expired@example.com", DisplayName: "Eve", Handle: "eve"}
	seed(t, conn, person)
	id := ask(t, svc, conn, person)
	if err := walk(t, svc, conn, acme); err != nil {
		t.Fatalf("walk: %v", err)
	}
	var link *contracts.Link
	if err := act(t, conn, person.ID, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		link, err = svc.Mint(ctx, tx, id, person.ID, false)
		return err
	}); err != nil {
		t.Fatalf("mint: %v", err)
	}

	// The clock moves past the seven days. Nothing swept the row: the refusal is what the
	// instant says at the moment the link is offered.
	clock.at = clock.at.Add(contracts.LinkLife + time.Minute)
	if err := transact(t, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := svc.Redeem(ctx, tx, link.Token)
		return err
	}); !errors.Is(err, contracts.ErrGrant) {
		t.Errorf("an expired link: got %v, want %v", err, contracts.ErrGrant)
	}
}

func TestTheSecondErasureRequestFilesOneRowAndAsksTheOwnersOnce(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, subject.Migrations)
	rec := &recorder{}
	svc := newDoor(&stamps{at: time.Date(2026, time.October, 9, 12, 0, 0, 0, time.UTC)}, &bundlesStub{}, &notesStub{}, rec)
	person := personaldata.Subject{ID: uuid.New(), Email: "again@example.com", DisplayName: "Ada", Handle: "again"}
	seed(t, conn, person)

	var ids []uuid.UUID
	for range 2 {
		var id uuid.UUID
		err := act(t, conn, person.ID, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			row, _, err := svc.RequestErase(ctx, tx, contracts.EraseInput{Subject: person.ID, Confirm: person.Email})
			if err != nil {
				return err
			}
			id = row.ID
			return err
		})
		if err != nil {
			t.Fatalf("erase: %v", err)
		}
		ids = append(ids, id)
	}
	if ids[0] != ids[1] {
		t.Errorf("the two requests filed %s and %s: two erasures of one person are one erasure", ids[0], ids[1])
	}
	if n := countErases(t, conn); n != 1 {
		t.Errorf("%d erasure rows for two requests", n)
	}
	asks := 0
	for _, a := range rec.asks {
		if a == "erase alpha" {
			asks++
		}
	}
	if asks != 1 {
		t.Errorf("the owner was asked to erase %d times, want once: %v", asks, rec.asks)
	}
}

// countErases counts the erase rows one tenant holds, for the case about redelivery.
func countErases(t *testing.T, conn *db.Conn) int {
	t.Helper()
	var n int64
	err := transact(t, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().WithContext(ctx).Model(&contracts.Request{}).
			Where("kind = ?", contracts.KindErase).Count(&n).Error
	})
	if err != nil {
		t.Fatalf("count the erasure rows: %v", err)
	}
	return int(n)
}

func TestOneTenantsRequestIsNotAnotherTenantsRow(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, subject.Migrations)
	beta := tenancy.Tenant{ID: uuid.New(), Slug: "beta", Name: "Beta"}
	svc := newDoor(&stamps{at: time.Date(2026, time.October, 9, 12, 0, 0, 0, time.UTC)}, &bundlesStub{}, &notesStub{})
	person := personaldata.Subject{ID: uuid.New(), Email: "acme.only@example.com", DisplayName: "Acme Person", Handle: "only"}
	seed(t, conn, person)
	id := ask(t, svc, conn, person)

	// The same commands, asked by another tenant: the row is not there, and the walk would
	// find nothing to do in Beta either. The bound is the policy, not a WHERE this module
	// wrote, and that is what the case is for.
	err := db.Run(tenancy.WithTenant(t.Context(), beta), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var n int64
		if err := tx.DB().WithContext(ctx).Model(&contracts.Request{}).Where("id = ?", id).Count(&n).Error; err != nil {
			return err
		}
		if n != 0 {
			t.Errorf("Beta can see %d of Acme's request rows", n)
		}
		if _, err := svc.Mint(ctx, tx, id, person.ID, true); !errors.Is(err, crud.ErrNotFound) {
			t.Errorf("minting across the tenant: got %v, want %v", err, crud.ErrNotFound)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("beta transaction: %v", err)
	}
	if err := walk(t, svc, conn, beta); err != nil {
		t.Fatalf("walking Beta: %v", err)
	}
	if row := readRequest(t, conn, id); row.State != contracts.StateRequested {
		t.Errorf("walking another tenant moved Acme's request to %q", row.State)
	}
}
