package internal_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/history"
	"github.com/septagon-oss/platformkit/modules/history/contracts"
	"github.com/septagon-oss/platformkit/modules/history/internal"
)

var acme = tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}
var globex = tenancy.Tenant{ID: uuid.New(), Slug: "globex", Name: "Globex"}

// page is one record's id. A version's record names no table this store owns —
// which records exist belongs to the module that owns the entity — so any id is a
// record as far as the version store is concerned, and every case below writes its
// versions against this one record.
var page = uuid.New()

// keep writes one version in the named tenant's own transaction.
func keep(t *testing.T, conn *db.Conn, tenant tenancy.Tenant, svc *internal.Service, in contracts.Input) error {
	t.Helper()
	return db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := svc.Record(ctx, tx, in)
		return err
	})
}

// read runs one assertion inside the named tenant's transaction.
func read(t *testing.T, conn *db.Conn, tenant tenancy.Tenant, fn func(ctx context.Context, tx db.Tx[db.Tenant]) error) {
	t.Helper()
	if err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, fn); err != nil {
		t.Fatal(err)
	}
}

// versionAt is one write of the shared record: at one number, one instant and one
// language, with the document that write committed.
func versionAt(number int64, at time.Time, locale, title string) contracts.Input {
	return contracts.Input{
		Module: "content", Entity: "page", Locale: locale, Record: page, Version: number,
		Fields:  json.RawMessage(`{"title":"` + title + `","body":"<p>` + title + `</p>"}`),
		Changed: []string{"title"}, At: at,
	}
}

// TestAVersionReadsBackAsTheWriteThatMadeItLeftIt is the store's whole promise in
// one read: the document a version holds is the bytes the write committed, and the
// number it is addressed by is the one the row carried. A store that normalised,
// reordered or filled in anything on the way in would answer with different bytes,
// and a restore of that version would write back something nobody ever wrote.
func TestAVersionReadsBackAsTheWriteThatMadeItLeftIt(t *testing.T) {
	_, conn := dbtest.Schema(t, history.Migrations)
	svc := internal.NewService(nil)
	at := time.Date(2026, 3, 4, 5, 6, 7, 8000, time.UTC)
	author := uuid.New()
	document := json.RawMessage(`{"body":"<p>second</p>","title":"Second draft   "}`)

	in := versionAt(2, at, "en-GB", "Second draft")
	in.Fields = document
	in.Changed = []string{"title", "body"}
	in.Reason = "restore:1"
	in.Actor = author
	if err := keep(t, conn, acme, svc, in); err != nil {
		t.Fatalf("keep version 2: %v", err)
	}

	read(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		got, err := svc.Version(ctx, tx, contracts.Key{
			Module: "content", Entity: "page", Locale: "en-GB", Record: page, Version: 2,
		})
		if err != nil {
			return err
		}
		// The same document, not the same bytes: the column is jsonb, which re-spells
		// what it is given and never what it says. A restore replays the values out of
		// this document, so the claim that matters is that no value moved.
		if !sameDocument(t, got.Fields, document) {
			t.Errorf("the version holds %s, want the document the write committed, %s", got.Fields, document)
		}
		if got.Version != 2 || got.Record != page || got.Module != "content" || got.Entity != "page" || got.Locale != "en-GB" {
			t.Errorf("the version is addressed as %s/%s %s in %s at %d", got.Module, got.Entity, got.Record, got.Locale, got.Version)
		}
		if want := []string{"title", "body"}; len(got.Changed) != len(want) || got.Changed[0] != want[0] || got.Changed[1] != want[1] {
			t.Errorf("the write moved %v, want %v", got.Changed, want)
		}
		if got.Reason != "restore:1" {
			t.Errorf("the version records the reason %q, want restore:1", got.Reason)
		}
		if got.Actor != author {
			t.Errorf("the version names %s as its author, want %s", got.Actor, author)
		}
		if !got.At.UTC().Equal(at) {
			t.Errorf("the version committed at %s, want %s", got.At, at)
		}
		if got.ID == uuid.Nil {
			t.Error("the version carries no id of its own, so nothing can hold a file for it")
		}
		return nil
	})
}

// TestAVersionNobodySignedInIsAnsweredAsNobodyAndNotAsAnId is the difference
// between the two answers the column has to carry: a write with no actor is a seed,
// a job or a system write, and it reads back as nobody. An all-zero id stored
// instead would read back as a person, and the panel would name one who never
// existed.
func TestAVersionNobodySignedInIsAnsweredAsNobodyAndNotAsAnId(t *testing.T) {
	_, conn := dbtest.Schema(t, history.Migrations)
	svc := internal.NewService(nil)
	if err := keep(t, conn, acme, svc, versionAt(1, time.Now().UTC(), "en", "Seeded")); err != nil {
		t.Fatalf("keep the seeded version: %v", err)
	}
	read(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		got, err := svc.Version(ctx, tx, contracts.Key{
			Module: "content", Entity: "page", Locale: "en", Record: page, Version: 1,
		})
		if err != nil {
			return err
		}
		if got.Actor != uuid.Nil {
			t.Errorf("a version nobody wrote names %s as its author", got.Actor)
		}
		return nil
	})
}

// TestTheSameVersionNumberTwiceIsRefusedAndTheFirstStands is the refusal one
// version cannot share with another. It is the store's answer rather than the
// database's leaking through, because a caller that has to recognise one recognises
// the other by the same name — and the version that stood is still there
// afterwards, since a refused write that took the standing one with it would be a
// loss of content, which rule 8 refuses outright.
func TestTheSameVersionNumberTwiceIsRefusedAndTheFirstStands(t *testing.T) {
	_, conn := dbtest.Schema(t, history.Migrations)
	svc := internal.NewService(nil)
	now := time.Now().UTC()
	if err := keep(t, conn, acme, svc, versionAt(2, now, "en", "First")); err != nil {
		t.Fatalf("keep version 2: %v", err)
	}
	if err := keep(t, conn, acme, svc, versionAt(2, now.Add(time.Minute), "en", "Second")); !errors.Is(err, contracts.ErrDuplicateNumber) {
		t.Fatalf("the second write of version 2: %v, want ErrDuplicateNumber", err)
	}

	read(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		items, err := svc.Page(ctx, tx, contracts.Query{
			Module: "content", Entity: "page", Locale: "en", Record: page, Limit: 10,
		})
		if err != nil {
			return err
		}
		if len(items) != 1 {
			t.Errorf("the refused write left %d versions where one stood", len(items))
			return nil
		}
		if items[0].Version != 2 || !sameDocument(t, items[0].Fields, versionAt(2, now, "en", "First").Fields) {
			t.Errorf("the standing version is %d holding %s, want version 2 holding the first document",
				items[0].Version, items[0].Fields)
		}
		return nil
	})
}

// TestAVersionSeriesComesNewestFirstAndItsSecondPageStartsBelowWhereTheFirstEnded
// is the panel's read: one record's versions in one language, newest first, with a
// continuation a reader can ask again with. A series that leaked another language's
// versions would compare two documents that are not of the same text, which is the
// mistake the per-locale column exists to prevent.
func TestAVersionSeriesComesNewestFirstAndItsSecondPageStartsBelowWhereTheFirstEnded(t *testing.T) {
	_, conn := dbtest.Schema(t, history.Migrations)
	svc := internal.NewService(nil)
	now := time.Now().UTC()
	for n := int64(1); n <= 6; n++ {
		if err := keep(t, conn, acme, svc, versionAt(n, now.Add(time.Duration(n)*time.Minute), "en", "Edit")); err != nil {
			t.Fatalf("keep version %d: %v", n, err)
		}
	}
	// The same record in another language, and the same language of another
	// record: neither belongs in the series read below.
	if err := keep(t, conn, acme, svc, versionAt(1, now, "pt-PT", "Edicao")); err != nil {
		t.Fatalf("keep the Portuguese version: %v", err)
	}
	other := versionAt(1, now, "en", "Another record's own")
	other.Record = uuid.New()
	if err := keep(t, conn, acme, svc, other); err != nil {
		t.Fatalf("keep the other record's version: %v", err)
	}

	read(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		first, err := svc.Page(ctx, tx, contracts.Query{
			Module: "content", Entity: "page", Locale: "en", Record: page, Limit: 2,
		})
		if err != nil {
			return err
		}
		if len(first) != 2 {
			return errors.New("the first page answered " + strconv.Itoa(len(first)) + " versions, want 2")
		}
		if first[0].Version != 6 || first[1].Version != 5 {
			return errors.New("the first page answered " + versionsOf(first) + ", want 6,5 newest first")
		}
		second, err := svc.Page(ctx, tx, contracts.Query{
			Module: "content", Entity: "page", Locale: "en", Record: page, Limit: 2, Before: first[1].Version,
		})
		if err != nil {
			return err
		}
		if len(second) != 2 || second[0].Version != 4 || second[1].Version != 3 {
			return errors.New("the page below version 5 answered " + versionsOf(second) + ", want 4,3")
		}
		locales := map[string]bool{}
		for _, v := range first {
			locales[v.Locale] = true
		}
		if len(locales) != 1 || !locales["en"] {
			t.Errorf("the English series answered the languages %v", locales)
		}
		return nil
	})
}

// TestAnotherTenantNeitherReadsNorWritesAVersion is the tenancy pillar's own case
// for this table, asked at the store rather than at an HTTP door: the series is not
// another tenant's to see, and no `WHERE` is doing that work — the row-level policy
// is, which is why a bare count over the table answers nothing either.
func TestAnotherTenantNeitherReadsNorWritesAVersion(t *testing.T) {
	_, conn := dbtest.Schema(t, history.Migrations)
	svc := internal.NewService(nil)
	if err := keep(t, conn, acme, svc, versionAt(1, time.Now().UTC(), "en", "Acme's draft")); err != nil {
		t.Fatalf("keep Acme's version: %v", err)
	}

	read(t, conn, globex, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := svc.Version(ctx, tx, contracts.Key{
			Module: "content", Entity: "page", Locale: "en", Record: page, Version: 1,
		})
		if !errors.Is(err, crud.ErrNotFound) {
			t.Errorf("Globex read Acme's version: %v, want ErrNotFound", err)
		}
		items, err := svc.Page(ctx, tx, contracts.Query{
			Module: "content", Entity: "page", Locale: "en", Record: page, Limit: 10,
		})
		if err != nil {
			return err
		}
		if len(items) != 0 {
			t.Errorf("Globex read %d of Acme's versions", len(items))
		}
		var seen int
		if err := tx.DB().Raw("SELECT count(*) FROM history_versions").Scan(&seen).Error; err != nil {
			return err
		}
		if seen != 0 {
			t.Errorf("Globex's transaction sees %d versions of a record it holds none of", seen)
		}
		return nil
	})
}

// TestAVersionWrittenForAnotherTenantIsRefusedByThePolicyAndNotByACheck is the
// WITH CHECK half, asked where only the database can answer it: a row naming one
// tenant while the transaction names another. The store never writes such a row —
// the tenant comes from the transaction and never from the input — so this is a
// test of the migration's policy, which is the half of tenancy a Go reviewer
// cannot see by reading the store.
func TestAVersionWrittenForAnotherTenantIsRefusedByThePolicyAndNotByACheck(t *testing.T) {
	_, conn := dbtest.Schema(t, history.Migrations)
	err := db.Run(tenancy.WithTenant(t.Context(), globex), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().WithContext(ctx).Exec(
			"INSERT INTO history_versions (tenant_id, module, entity, record_id, locale, version, fields, committed_at)"+
				" VALUES (?, 'content', 'page', ?, 'en', 1, '{}', ?)",
			acme.ID, page, time.Now().UTC()).Error
	})
	if err == nil {
		t.Fatal("a version written for Acme inside Globex's transaction was accepted; the policy's WITH CHECK half is not there")
	}
}

// sameDocument answers whether two spellings of a stored row hold the same
// values. The column is jsonb, which re-spells what it is given — the order of
// keys, the space after a colon — and a version that could not survive its own
// column would be no use to a restore, so the claim is equality of content.
func sameDocument(t *testing.T, stored, written json.RawMessage) bool {
	t.Helper()
	var a, b any
	if err := json.Unmarshal(stored, &a); err != nil {
		t.Fatalf("the stored version holds %s, which is not a document: %v", stored, err)
	}
	if err := json.Unmarshal(written, &b); err != nil {
		t.Fatalf("the write committed %s, which is not a document: %v", written, err)
	}
	return reflect.DeepEqual(a, b)
}

func versionsOf(items []contracts.Version) string {
	out := ""
	for i, v := range items {
		if i > 0 {
			out += ","
		}
		out += strconv.FormatInt(v.Version, 10)
	}
	return out
}
