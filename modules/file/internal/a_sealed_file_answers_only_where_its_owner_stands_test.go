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

// The sealed flag is the file module's own boundary — bytes whose door belongs to the
// module that stored them — so the flag is checked here, at the module, and not only where
// the composed application happens to notice. What this case holds fixed is which door
// closes: the presigned URL, which is the content route with the signature done by the
// store, has to answer the same refusal. `Open` still answers, and that is the half that
// keeps the design a door rather than a wall — the module that sealed the bytes is exactly
// who that answer is for.

// uploadSealed files one text body, sealed or not, and hands back the row.
func uploadSealed(t *testing.T, svc contracts.Service, conn *db.Conn, name string, sealed bool) *contracts.File {
	t.Helper()
	var f *contracts.File
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		f, err = svc.Upload(ctx, held(tx), contracts.Upload{
			Name: name, ContentType: "text/plain", Declared: -1, Sealed: sealed,
			Body: strings.NewReader("the bytes of " + name),
		})
		return err
	})
	if err != nil {
		t.Fatalf("upload %s: %v", name, err)
	}
	return f
}

func granted(t *testing.T, conn *db.Conn, svc contracts.Service, id uuid.UUID) error {
	t.Helper()
	return db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := svc.Grant(ctx, tx, id, 0)
		return err
	})
}

func TestASealedFileHasNoDoorOfThisModule(t *testing.T) {
	_, conn := dbtest.Schema(t, file.Migrations)
	svc := internal.NewService(internal.NewLocal(t.TempDir()), filetest.Limit, 0, 0)
	ordinary := uploadSealed(t, svc, conn, "notes.txt", false)
	sealed := uploadSealed(t, svc, conn, "export-bundle.zip", true)

	in := tenancy.WithTenant(t.Context(), acme)
	if err := db.Run(in, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		f, err := crud.Get[*contracts.File](tx, sealed.ID)
		if err != nil {
			return err
		}
		if !f.Sealed {
			t.Errorf("the upload that sealed its bytes stored a row saying it did not")
		}
		g, err := crud.Get[*contracts.File](tx, ordinary.ID)
		if err != nil {
			return err
		}
		if g.Sealed {
			t.Errorf("an upload that named nothing stored a sealed row; the flag is the storing module's to stamp")
		}
		return nil
	}); err != nil {
		t.Fatalf("read the rows back: %v", err)
	}

	// The presigned door closes with the other one, and with the answer a stranger gets:
	// a sealed file's existence is its owning module's to confirm.
	if err := granted(t, conn, svc, sealed.ID); !errors.Is(err, crud.ErrNotFound) {
		t.Errorf("a presigned URL for a sealed file: got %v, want %v", err, crud.ErrNotFound)
	}
	// The same call on an ordinary private file reaches the store's own answer — this
	// deployment's disk store cannot sign and says so — which is what proves the refusal
	// above came from the sealed flag and not from the store.
	if err := granted(t, conn, svc, ordinary.ID); errors.Is(err, crud.ErrNotFound) {
		t.Errorf("the private file was refused as though it were sealed: %v", err)
	}

	// And the owning module still has its read. The doors that leaked are the two content
	// routes, which answer a caller rather than a module; the case that holds them to the
	// refusal is the composed application's, where a real route, a real scoped key and a
	// real bundle exist to ask.
	if err := db.Run(in, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, body, err := svc.Open(ctx, tx, sealed.ID, false)
		if err != nil {
			return err
		}
		return body.Close()
	}); err != nil {
		t.Errorf("the module that sealed a file cannot read it back: %v", err)
	}
}
