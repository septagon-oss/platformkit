package file_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/jobs"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/content"
	contentcontracts "github.com/septagon-oss/platformkit/modules/content/contracts"
	"github.com/septagon-oss/platformkit/modules/file"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
	"github.com/septagon-oss/platformkit/modules/history"
	"github.com/septagon-oss/platformkit/modules/translation/contracts/translationtest"
	"github.com/septagon-oss/platformkit/pkit"
)

// TestDeletingThePublishedRecordMakesItsImagePrivateAgain pins the third way
// back the brief decides: "unpublishing, or deleting the last public user,
// turns them back". Publishing flips the image public; deleting the one record
// that showed it ends its uses (kit/rest's delete clears every richtext use the
// record filed) and must leave nothing public — an anonymous read after the
// delete finds no image, because the only public user of those bytes is gone.
func TestDeletingThePublishedRecordMakesItsImagePrivateAgain(t *testing.T) {
	// The version table comes along with the page table: the composition
	// below names the keeper of versions, because content's writes keep a
	// version of every save, and a plan that named none would be refused.
	_, conn := dbtest.Schema(t, file.Migrations, content.Migrations, history.Migrations)
	store := file.Local(t.TempDir())
	inputs := pkit.NewModule("inputs", func(w *pkit.Wiring) (module.Module, error) {
		who := caller{hosts: map[string]tenancy.Tenant{host: acme}}
		pkit.Put[httpx.TenantLoader](w, who)
		pkit.Put[httpx.Authorizer](w, who)
		pkit.Put[pkit.Authenticator](w, func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{}, false, nil
		})
		pkit.Put[contracts.Storage](w, store)
		pkit.Put[jobs.TenantLister](w, widthsOnly{walking: []tenancy.Tenant{acme}})
		// The record's door onto the translation table, which content.Module
		// needs since its own title and body became translatable. This case asks
		// who may read a picture, so the port is answered by the translation
		// module's own test double and is never written through.
		pkit.Put[rest.Translations](w, translationtest.NewFake(nil, translationtest.NewStubSource()))
		return module.Module{Name: "inputs"}, nil
	}, pkit.Provides[httpx.TenantLoader](), pkit.Provides[httpx.Authorizer](),
		pkit.Provides[pkit.Authenticator](), pkit.Provides[contracts.Storage](),
		pkit.Provides[jobs.TenantLister](), pkit.Provides[rest.Translations]())
	plan, err := pkit.NewApp("illustrated").Use(inputs, file.Module, history.Module, content.Module).Plan(pkit.Deployment{
		Environment: pkit.Development, Config: config.Config{Files: config.Files{MaxBytes: 16 << 20}},
	})
	if err != nil {
		t.Fatal(err)
	}
	files, ok := pkit.Value[contracts.Service](plan)
	if !ok {
		t.Fatal("file provider omitted its service")
	}
	pages, ok := pkit.Value[contentcontracts.Service](plan)
	if !ok {
		t.Fatal("content provider omitted its service")
	}
	author := tenancy.WithActor(tenancy.WithTenant(t.Context(), acme), uuid.New())
	editor := tenancy.WithActor(tenancy.WithTenant(t.Context(), acme), uuid.New())
	use := contracts.Use{Module: "content", Entity: "content", Field: "body", Locale: "en"}
	var uploaded *contracts.File
	page := &contentcontracts.Content{Slug: "illustrated-once", Title: "Illustrated once"}
	if err := db.Run(author, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		picture := wideFrame(t)
		var err error
		uploaded, err = files.Upload(ctx, func(context.Context) (db.Tx[db.Tenant], error) { return tx, nil }, contracts.Upload{
			Name: "wide.jpg", ContentType: "image/jpeg", Image: true, Body: bytes.NewReader(picture), Declared: int64(len(picture)),
		})
		if err != nil {
			return err
		}
		page.Body = "![Landscape](pk-file:" + uploaded.ID.String() + ")"
		if err := crud.Create(ctx, tx, page); err != nil {
			return err
		}
		use.Record = page.ID
		_, err = files.SetUses(ctx, tx, use, []uuid.UUID{uploaded.ID})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Run(editor, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if _, err := pages.Publish(ctx, tx, page.ID); err != nil {
			return err
		}
		// The probe: the publish flipped the image, which is this branch's own
		// cured behaviour, so the delete below starts from a public image.
		row, body, err := files.Open(ctx, tx, uploaded.ID, true)
		if err != nil {
			return err
		}
		body.Close()
		if !row.Public() {
			t.Fatal("published image's row is private; the delete below would prove nothing")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// Delete the one record that shows the image, as kit/rest's generated delete
	// does: end every use its richtext fields filed, then remove the row, in one
	// transaction.
	if err := db.Run(editor, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if _, err := files.SetUses(ctx, tx, use, nil); err != nil {
			return err
		}
		return crud.Delete[*contentcontracts.Content](tx, page.ID, false)
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Run(editor, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, body, err := files.Open(ctx, tx, uploaded.ID, true)
		if body != nil {
			body.Close()
		}
		if !errors.Is(err, crud.ErrNotFound) {
			t.Errorf("anonymous read after the only record showing the image was deleted: %v, want %v", err, crud.ErrNotFound)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
