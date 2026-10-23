package filetest

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
)

// A body replaces its references through CollapseRefs and DiffUses.
func replaceRecordUses(t *testing.T, f Fixture) {
	first, second := stored(t, f, "one"), stored(t, f, "two")
	record := uuid.New()
	use := contracts.Use{Module: "content", Entity: "page", Record: record, Field: "body", Locale: "en"}
	// A repeat collapses: one field showing the same image twice is one
	// use of it, because the file is kept alive once. And nothing is
	// published: the record's own write is the auditable fact, and the
	// file module has no business emitting an event for somebody else's
	// row, so the suite asks that no event arrived at all.
	before := len(f.Published())
	unused, err := f.Service.SetUses(f.Ctx, f.Tx, use, []uuid.UUID{first.ID, second.ID, first.ID})
	if err != nil {
		t.Fatalf("SetUses: %v", err)
	}
	if got := f.Published()[before:]; len(got) != 0 {
		t.Errorf("recording a use published %v, want nothing", got)
	}
	if len(unused) != 0 {
		t.Errorf("the first write ended %d uses, want none", len(unused))
	}
	rows, err := f.Service.Uses(f.Ctx, f.Tx, first.ID)
	if err != nil {
		t.Fatalf("Uses: %v", err)
	}
	if len(rows) != 1 || rows[0].Use != use {
		t.Errorf("the file is used by %+v, want the one field that named it", rows)
	}
	// The edit that dropped the first image ends its use and names it,
	// and the row does not linger: a use that outlived the body that
	// stopped naming the file is the answer "nothing reads this" being
	// wrong about a file the sweep is about to release.
	out, err := f.Service.SetUses(f.Ctx, f.Tx, use, []uuid.UUID{second.ID})
	if err != nil {
		t.Fatalf("SetUses again: %v", err)
	}
	if len(out) != 1 || out[0] != first.ID {
		t.Errorf("the rewrite ended %v, want the file the body dropped", out)
	}
	if rows, err := f.Service.Uses(f.Ctx, f.Tx, first.ID); err != nil || len(rows) != 0 {
		t.Errorf("the dropped file is still read by %+v (%v), want nobody", rows, err)
	}
	// A body with no images at all leaves nothing, and the second image
	// is still read by the same field.
	if _, err := f.Service.SetUses(f.Ctx, f.Tx, use, nil); err != nil {
		t.Fatalf("a body that shows nothing: %v", err)
	}
	if rows, err := f.Service.Uses(f.Ctx, f.Tx, second.ID); err != nil || len(rows) != 0 {
		t.Errorf("after the body dropped both images it reads %+v (%v), want nobody", rows, err)
	}
}

func refuseMissingUse(t *testing.T, f Fixture) {
	kept := stored(t, f, "kept")
	use := contracts.Use{Module: "content", Entity: "page", Record: uuid.New(), Field: "body", Locale: "en"}
	_, err := f.Service.SetUses(f.Ctx, f.Tx, use, []uuid.UUID{kept.ID, uuid.New()})
	if !errors.Is(err, crud.ErrNotFound) {
		t.Fatalf("a body naming a gone file = %v, want ErrNotFound", err)
	}
	if !strings.Contains(err.Error(), "file") {
		t.Errorf("the refusal does not name the file it refused: %v", err)
	}
	// The id that was there is not half-written: a use of the files that
	// happen to still exist would say a record shows what it does not.
	rows, uerr := f.Service.Uses(f.Ctx, f.Tx, kept.ID)
	if uerr != nil || len(rows) != 0 {
		t.Errorf("the refused write left %+v (%v), want nothing", rows, uerr)
	}
}

// Removing a reference must succeed even after its file has gone.
func endUseOfDeletedFile(t *testing.T, f Fixture) {
	gone, kept := stored(t, f, "shown then removed"), stored(t, f, "still shown")
	use := contracts.Use{Module: "content", Entity: "page", Record: uuid.New(), Field: "body", Locale: "en"}
	if _, err := f.Service.SetUses(f.Ctx, f.Tx, use, []uuid.UUID{gone.ID, kept.ID}); err != nil {
		t.Fatalf("SetUses: %v", err)
	}
	// Not Delete: that door refuses a file a record still shows, which is the
	// refusal in the case below. This is the state the erasure door and the
	// retention sweep leave behind, and the row they leave is what the write
	// here is for.
	f.Remove(gone.ID)
	if _, err := f.Service.Delete(f.Ctx, f.Tx, gone.ID); !errors.Is(err, crud.ErrNotFound) {
		t.Fatalf("the removed file answers %v, want ErrNotFound", err)
	}
	if _, err := f.Service.SetUses(f.Ctx, f.Tx, use, []uuid.UUID{kept.ID}); err != nil {
		t.Fatalf("the edit that dropped a removed image = %v, want it accepted", err)
	}
	if rows, err := f.Service.Uses(f.Ctx, f.Tx, kept.ID); err != nil || len(rows) != 1 || rows[0].Use != use {
		t.Errorf("the file still shown reads by %+v (%v), want the one field showing it", rows, err)
	}
	// The same rewrite again is the test that the dangling row went with
	// the first one: were it still there, this call would end it a second
	// time and name it as newly unused.
	out, err := f.Service.SetUses(f.Ctx, f.Tx, use, []uuid.UUID{kept.ID})
	if err != nil || len(out) != 0 {
		t.Errorf("rewriting the same body again ended %v (%v), want nothing", out, err)
	}
}

// The ledger refuses deletion until the last record releases the file.
func refuseUsedFile(t *testing.T, f Fixture) {
	row := stored(t, f, hello)
	use := contracts.Use{Module: "content", Entity: "page", Record: uuid.New(), Field: "body", Locale: "en"}
	if _, err := f.Service.SetUses(f.Ctx, f.Tx, use, []uuid.UUID{row.ID}); err != nil {
		t.Fatalf("SetUses: %v", err)
	}
	before := len(f.Published())
	keys := len(f.Keys())
	_, err := f.Service.Delete(f.Ctx, f.Tx, row.ID)
	if !errors.Is(err, contracts.ErrInUse) {
		t.Fatalf("Delete of a shown file = %v, want ErrInUse", err)
	}
	refused := new(contracts.InUse)
	if !errors.As(err, &refused) || refused.File != row.ID || refused.Records != 1 {
		t.Errorf("the refusal is %+v, want this file and one record", refused)
	}
	// Nothing was written, published or unlinked: a refusal is a refusal, and
	// the bytes stay where the record showing them can still read them.
	if got := f.Published()[before:]; len(got) != 0 {
		t.Errorf("the refusal published %v, want nothing", got)
	}
	if len(f.Keys()) != keys {
		t.Errorf("the refusal removed a blob, want every key still there")
	}
	if _, back := read(t, f, row.ID, false); back != hello {
		t.Errorf("the refused file reads %q, want %q", back, hello)
	}
	// The record stops showing it, and now the file is nobody's to protect.
	if _, err := f.Service.SetUses(f.Ctx, f.Tx, use, nil); err != nil {
		t.Fatalf("ending the use: %v", err)
	}
	f.one(t, "deleting it then", contracts.EventDeleted, func() {
		if _, err := f.Service.Delete(f.Ctx, f.Tx, row.ID); err != nil {
			t.Fatalf("Delete once nobody shows it: %v", err)
		}
	})
}

// Multiple uses by one record still name one record to edit.
func listUsingRecords(t *testing.T, f Fixture) {
	row := stored(t, f, hello)
	one, two := uuid.New(), uuid.New()
	uses := []contracts.Use{
		{Module: "content", Entity: "page", Record: one, Field: "body", Locale: "en"},
		{Module: "content", Entity: "page", Record: one, Field: "summary", Locale: "en"},
		{Module: "content", Entity: "page", Record: one, Field: "body", Locale: "fr"},
		{Module: "site", Entity: "logo", Record: two, Field: "body", Locale: "en"},
	}
	for _, use := range uses {
		if _, err := f.Service.SetUses(f.Ctx, f.Tx, use, []uuid.UUID{row.ID}); err != nil {
			t.Fatalf("SetUses %s/%s: %v", use.Module, use.Field, err)
		}
	}
	if rows, err := f.Service.Uses(f.Ctx, f.Tx, row.ID); err != nil || len(rows) != 4 {
		t.Fatalf("the ledger holds %+v (%v), want the four rows this wrote", rows, err)
	}
	_, err := f.Service.Delete(f.Ctx, f.Tx, row.ID)
	refused := new(contracts.InUse)
	if !errors.As(err, &refused) || refused.Records != 2 {
		t.Errorf("four rows over two records = %v, want a refusal naming two records", err)
	}
}

// A public record makes an added file readable in the write that adds it.
func publishAddedUse(t *testing.T, f Fixture) {
	row := stored(t, f, hello)
	uses := contracts.Use{Module: "content", Entity: "page", Record: uuid.New(), Field: "body", Locale: "en"}
	f.one(t, "filing a published use", contracts.EventVisibility, func() {
		if _, err := f.Service.(contracts.ShownUses).SetUsesShown(f.Ctx, f.Tx, uses, []uuid.UUID{row.ID}, true); err != nil {
			t.Fatalf("SetUsesShown: %v", err)
		}
	})
	out, _, err := f.Service.Open(f.Ctx, f.Tx, row.ID, true)
	if err != nil {
		t.Errorf("an anonymous read of the file a published record shows: %v", err)
	} else if out.Visibility != contracts.VisibilityPublic {
		t.Errorf("visibility is %q, want public", out.Visibility)
	}
}

// A private use neither publishes a file nor retracts its uploaded visibility.
func keepUnshownUsePrivate(t *testing.T, f Fixture) {
	private := stored(t, f, "a draft's picture")
	hand, err := f.Service.Upload(f.Ctx, f.open, upload("logo.png", "image/png", contracts.VisibilityPublic, png))
	if err != nil {
		t.Fatalf("a public upload: %v", err)
	}
	uses := contracts.Use{Module: "content", Entity: "page", Record: uuid.New(), Field: "body", Locale: "en"}
	before := len(f.Published())
	if _, err := f.Service.SetUses(f.Ctx, f.Tx, uses, []uuid.UUID{private.ID, hand.ID}); err != nil {
		t.Fatalf("SetUses: %v", err)
	}
	if got := f.Published()[before:]; len(got) != 0 {
		t.Errorf("a private rewrite published %v, want nothing", got)
	}
	for _, id := range []uuid.UUID{private.ID, hand.ID} {
		row, _, err := f.Service.Open(f.Ctx, f.Tx, id, false)
		if err != nil {
			t.Fatalf("read %s: %v", id, err)
		}
		want := contracts.VisibilityPrivate
		if id == hand.ID {
			want = contracts.VisibilityPublic
		}
		if row.Visibility != want {
			t.Errorf("%s is %q, want %q", id, row.Visibility, want)
		}
	}
}

// Ending the last public use restores the file's uploaded visibility.
func privatiseLastPublicUse(t *testing.T, f Fixture) {
	row := stored(t, f, hello)
	uses := contracts.Use{Module: "content", Entity: "page", Record: uuid.New(), Field: "body", Locale: "en"}
	if _, err := f.Service.(contracts.ShownUses).SetUsesShown(f.Ctx, f.Tx, uses, []uuid.UUID{row.ID}, true); err != nil {
		t.Fatalf("SetUsesShown: %v", err)
	}
	f.one(t, "rewriting the body without it", contracts.EventVisibility, func() {
		if _, err := f.Service.SetUses(f.Ctx, f.Tx, uses, nil); err != nil {
			t.Fatalf("ending the use: %v", err)
		}
	})
	if _, _, err := f.Service.Open(f.Ctx, f.Tx, row.ID, true); !errors.Is(err, crud.ErrNotFound) {
		t.Errorf("an anonymous read after the last public use ended = %v, want not found", err)
	}
}
