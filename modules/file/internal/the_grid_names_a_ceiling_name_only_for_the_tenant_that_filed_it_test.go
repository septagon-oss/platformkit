package internal_test

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/file"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
	"github.com/septagon-oss/platformkit/modules/file/internal"
)

// TestTheGridNamesACeilingNameOnlyForTheTenantThatFiledIt is the ground the
// ceiling case stands on when it reads the library grid. The name is the ceiling
// exactly, and the box is capped below it, so the only question the box can ask
// about that file is a fragment of its name. This asks that one question as both
// tenants: the tenant that filed the picture is answered its one tile, named with
// the whole name, which is what says the grid read is reached at all; the tenant
// that did not is answered nothing and told nothing about having refused.
//
// It is a separate claim from the row's own name: a grid that searched before the
// row-level policy ran would answer both tenants here and the row read would stay
// green, and a grid whose pattern never matches a name at the ceiling would answer
// nobody and look like a shortened label.
func TestTheGridNamesACeilingNameOnlyForTheTenantThatFiledIt(t *testing.T) {
	_, conn := dbtest.Schema(t, file.Migrations)
	dir := t.TempDir()
	svc := internal.NewService(internal.NewLocal(dir), 16<<20, 0, 0)
	globex := tenancy.Tenant{ID: uuid.New(), Slug: "globex", Name: "Globex"}

	var picture bytes.Buffer
	if err := jpeg.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 40, 20)), nil); err != nil {
		t.Fatal(err)
	}
	name := strings.Repeat("é", contracts.MaxName)
	// Below contracts.MaxQuery, which is why the box cannot ask for the whole name.
	fragment := strings.Repeat("é", 20)

	// Each tenant asks in its own transaction, the way a page does.
	type answer struct {
		rows  int
		total int64
		named string
	}
	ask := func(tenant tenancy.Tenant) (answer, error) {
		var got answer
		err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			rows, total, err := svc.Search(ctx, tx, contracts.LibraryQuery{Name: fragment})
			if err != nil {
				return err
			}
			got.rows, got.total = len(rows), total
			if len(rows) == 1 {
				got.named = rows[0].Name
			}
			return nil
		})
		return got, err
	}

	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := svc.Upload(ctx, held(tx), contracts.Upload{
			Name: name, ContentType: "image/jpeg",
			Body: bytes.NewReader(picture.Bytes()), Declared: int64(picture.Len()),
		})
		return err
	})
	if err != nil {
		t.Fatalf("filing a %d-character name = %v", contracts.MaxName, err)
	}

	owner, err := ask(acme)
	if err != nil {
		t.Fatalf("acme searching a fragment of the name it filed = %v", err)
	}
	if owner.rows != 1 || owner.total != 1 {
		t.Fatalf("acme searching its own ceiling name was answered %d of %d tiles, want the one it filed",
			owner.rows, owner.total)
	}
	if owner.named != name {
		t.Errorf("the grid names acme's file %q, want the whole %d-character name the row keeps",
			owner.named, contracts.MaxName)
	}

	other, err := ask(globex)
	if err != nil {
		t.Fatalf("globex searching the same fragment = %v", err)
	}
	if other.rows != 0 || other.total != 0 {
		t.Errorf("globex searching a fragment of acme's ceiling name was answered %d of %d tiles, want nothing",
			other.rows, other.total)
	}
}
