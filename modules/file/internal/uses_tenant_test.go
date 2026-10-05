package internal_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/file"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
	"github.com/septagon-oss/platformkit/modules/file/contracts/filetest"
	"github.com/septagon-oss/platformkit/modules/file/internal"
)

// A use is written inside the tenant that wrote the body, and it names a file in
// that same tenant. Nothing in Service.SetUses asks whose file an id is: the row
// it would name is simply not there for this transaction to find, which is what
// makes a use of another tenant's file impossible rather than disfavoured.
//
// The second half of the case is the quieter one. A row in file_uses carries a
// tenant_id, and the policy's WITH CHECK half is what refuses a row written for
// one tenant by another's transaction — so this is a write test, not a read test,
// and it is the write that had better fail.
func TestAnotherTenantsFileCannotBeUsedOrListed(t *testing.T) {
	_, conn := dbtest.Schema(t, file.Migrations)
	a, b := tenancy.Tenant{ID: uuid.New(), Slug: "a"}, tenancy.Tenant{ID: uuid.New(), Slug: "b"}
	dir := t.TempDir()
	store := internal.NewLocal(dir)
	svc := internal.NewService(store, filetest.Limit, 0, 0)

	held := func(tx db.Tx[db.Tenant]) contracts.Tx {
		return func(context.Context) (db.Tx[db.Tenant], error) { return tx, nil }
	}
	// A's file, uploaded through the service so the row and the bytes agree.
	var fileID uuid.UUID
	if err := db.Run(tenancy.WithTenant(t.Context(), a), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		row, err := svc.Upload(ctx, held(tx), uploaded("a.png"))
		if err != nil {
			return err
		}
		fileID = row.ID
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	use := contracts.Use{Module: "content", Entity: "page", Record: uuid.New(), Field: "body", Locale: "en"}

	// B cannot record a use of it.
	if err := db.Run(tenancy.WithTenant(t.Context(), b), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := svc.SetUses(ctx, tx, use, []uuid.UUID{fileID})
		if !errors.Is(err, crud.ErrNotFound) {
			t.Errorf("B used A's file: %v, want ErrNotFound", err)
			return nil
		}
		// And it cannot read who reads it, which is the same door from the other
		// side: an empty list would be an answer about A's file.
		if _, err := svc.Uses(ctx, tx, fileID); !errors.Is(err, crud.ErrNotFound) {
			t.Errorf("B listed A's uses: %v, want ErrNotFound", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// A's own use of its own file still works, and it is the only row there is.
	if err := db.Run(tenancy.WithTenant(t.Context(), a), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if _, err := svc.SetUses(ctx, tx, use, []uuid.UUID{fileID}); err != nil {
			return err
		}
		rows, err := svc.Uses(ctx, tx, fileID)
		if err != nil {
			return err
		}
		if len(rows) != 1 || rows[0].Use != use {
			t.Errorf("A's uses = %+v, want the one field that named it", rows)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// B's refused attempt wrote nothing at all: not a use row, and not a second
	// tenant's claim on A's file. The count is read as the owner, because that is
	// the only transaction that can see the row.
	if err := db.Run(tenancy.WithTenant(t.Context(), b), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var mine int64
		if err := tx.DB().Model(&contracts.FileUse{}).Where("record = ?", use.Record).
			Count(&mine).Error; err != nil {
			return err
		}
		if mine != 0 {
			t.Errorf("B's refused write left %d use rows, want none", mine)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// Two records that both reference the same two files rewrite their own uses, in
// either order, and the outcome is the set the last accepted body named rather
// than a mixture of the two. The locks are taken in ascending id order precisely
// so that this settles rather than deadlocks (internal/service.go, SetUses).
func TestTwoRecordsRecordingTheSameFilesSettle(t *testing.T) {
	_, conn := dbtest.Schema(t, file.Migrations)
	dir := t.TempDir()
	svc := internal.NewService(internal.NewLocal(dir), filetest.Limit, 0, 0)

	ctx := tenancy.WithTenant(t.Context(), acme)
	var first, second uuid.UUID
	var a, b uuid.UUID // the two records
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		for _, name := range []string{"one.png", "two.png"} {
			row, err := svc.Upload(ctx, held(tx), uploaded(name))
			if err != nil {
				return err
			}
			if name == "one.png" {
				first = row.ID
			} else {
				second = row.ID
			}
		}
		a, b = uuid.New(), uuid.New()
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// Both writers start from the same world — each already shows both files —
	// and each ends with one of them. Whoever wins, the row set is one body's,
	// and a third read sees what the winner wrote.
	seed := func(record uuid.UUID, refs ...uuid.UUID) {
		if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			_, err := svc.SetUses(ctx, tx, useOf(record), refs)
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	seed(a, first, second)
	seed(b, first, second)

	done := make(chan error, 2)
	writes := [][]uuid.UUID{{first}, {second}}
	records := []uuid.UUID{a, b}
	for i := range records {
		go func(i int) {
			done <- db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				_, err := svc.SetUses(ctx, tx, useOf(records[i]), writes[i])
				return err
			})
		}(i)
	}
	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Fatalf("both writers were expected to settle; one failed: %v", err)
		}
	}

	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		// The invariant is not which one won but that the state is a state: each
		// record's uses are exactly the set one of the two bodies named, and the
		// same read answers twice.
		for i, record := range records {
			rows, err := svc.Uses(ctx, tx, writes[i][0])
			if err != nil {
				return err
			}
			found := 0
			for _, row := range rows {
				if row.Use.Record == record {
					found++
				}
			}
			if found != 1 {
				t.Errorf("record %d reads %d rows of its own file, want one", i, found)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// uploaded is a private upload of a body that sniffs as a PNG. It is eight
// bytes and not a frame on purpose: what a use is a use of is a row, and this
// file is about who may name one. See contracts/image.go for the frame cases.
func uploaded(name string) contracts.Upload {
	return contracts.Upload{
		Name: name, ContentType: "image/png", Visibility: contracts.VisibilityPrivate,
		Declared: -1, Body: strings.NewReader("\x89PNG\r\n\x1a\n"),
	}
}

func useOf(record uuid.UUID) contracts.Use {
	return contracts.Use{Module: "content", Entity: "page", Record: record, Field: "body", Locale: "en"}
}
