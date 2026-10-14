package internal_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/file"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
	"github.com/septagon-oss/platformkit/modules/file/internal"
)

// TestTheLibraryReadsOneTenantAndChargesThatOne is the grid, the meter and the
// caption against the row-level policy and against the encode.
//
// Three claims the fake cannot answer live here. A search finds only the
// searching tenant's rows — the pattern is applied after the policy, not before
// it. The meter is the sum an upload is refused against *including the other
// widths of the same pictures*, which is the half of the sum only the encode
// writes. And a caption written against a row two writes have moved is refused
// by the lock, not by the fake's map: two SetAlts on one row settle one at a
// time and the loser is told, in the same breath, what the row now says.
func TestTheLibraryReadsOneTenantAndChargesThatOne(t *testing.T) {
	_, conn := dbtest.Schema(t, file.Migrations)
	globex := tenancy.Tenant{ID: uuid.New(), Slug: "globex", Name: "Globex"}
	store := internal.NewLocal(t.TempDir())

	upload := func(tenant tenancy.Tenant, name, contentType string, body []byte) *contracts.File {
		t.Helper()
		var out *contracts.File
		err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			var err error
			out, err = internal.NewService(store, 16<<20, 0, 0).Upload(ctx, held(tx), contracts.Upload{
				Name: name, ContentType: contentType, Declared: int64(len(body)), Body: bytes.NewReader(body),
			})
			return err
		})
		if err != nil {
			t.Fatalf("upload %s as %s: %v", name, tenant.Slug, err)
		}
		return out
	}
	jpegOf := func(w, h int) []byte {
		t.Helper()
		frame := image.NewRGBA(image.Rect(0, 0, w, h))
		for x := range w {
			for y := range h {
				frame.Set(x, y, color.RGBA{R: uint8(x * y), G: uint8(y), B: uint8(x), A: 255})
			}
		}
		var out bytes.Buffer
		if err := jpeg.Encode(&out, frame, &jpeg.Options{Quality: 85}); err != nil {
			t.Fatalf("encode the picture: %v", err)
		}
		return out.Bytes()
	}

	// One picture and one note for each of two tenants, with a name the other
	// tenant will search for. Nothing here needs a variant to be built yet.
	acmePicture := upload(acme, "acme-cover.jpg", "image/jpeg", jpegOf(400, 300))
	upload(acme, "acme-notes.txt", "text/plain", []byte("notes\n"))
	globexPicture := upload(globex, "acme-cover.jpg", "image/jpeg", jpegOf(200, 200))
	_ = upload(globex, "globex-notes.txt", "text/plain", []byte("notes\n"))

	// The grid, as acme. The policy runs before the pattern and before the limit,
	// so a search for a name that exists in the other tenant answers nothing and
	// says nothing about having refused to find it.
	var searched []*contracts.File
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		svc := internal.NewService(store, 16<<20, 0, 0)
		rows, total, err := svc.Search(ctx, tx, contracts.LibraryQuery{Name: "acme-cover"})
		if err != nil {
			return err
		}
		if total != 1 || len(rows) != 1 || rows[0].ID != acmePicture.ID {
			t.Errorf("acme searching the name both tenants have got %d of %d, want its own one row", len(rows), total)
		}
		if _, _, err := svc.Search(ctx, tx, contracts.LibraryQuery{Family: "pictures"}); !errors.Is(err, crud.ErrInvalid) {
			t.Errorf("a family the library does not offer = %v, want ErrInvalid naming the three it does", err)
		}
		if _, err := crud.Get[*contracts.File](tx, globexPicture.ID); !errors.Is(err, crud.ErrNotFound) {
			t.Errorf("acme read globex's file by id: %v, want not found", err)
		}
		searched = rows
		return nil
	})
	if err != nil {
		t.Fatalf("acme's grid: %v", err)
	}
	if len(searched) != 1 || searched[0].Alt != "" || searched[0].Decorative {
		t.Errorf("a freshly uploaded file arrives with %+v, want no caption and no claim", searched[0])
	}

	// The meter, over a tenant whose widths really were built. The picture and
	// its other widths are one tenant's disk, and a meter that counted the frame
	// alone would promise the difference between them.
	svc := internal.NewService(store, 16<<20, 0, 0)
	big := upload(acme, "wide.jpg", "image/jpeg", jpegOf(1000, 500))
	jobs := internal.NewVariants(svc, store, sweepWalk{walking: []tenancy.Tenant{acme}}, time.Second).Jobs()
	if err := jobs[0].Run(t.Context(), conn); err != nil {
		t.Fatalf("the encode pass: %v", err)
	}
	var meter *contracts.Quota
	if err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		if meter, err = svc.Quota(ctx, tx); err != nil {
			return err
		}
		var frames, widths int64
		if err := tx.DB().Model(&contracts.File{}).Where("deleted_at IS NULL").
			Select("COALESCE(SUM(size),0)").Scan(&frames).Error; err != nil {
			return err
		}
		if err := tx.DB().Model(&contracts.Variant{}).Where("deleted_at IS NULL").
			Select("COALESCE(SUM(size),0)").Scan(&widths).Error; err != nil {
			return err
		}
		if meter.Used != frames+widths {
			t.Errorf("the meter says %d bytes, want the frames' %d plus their widths' %d", meter.Used, frames, widths)
		}
		if meter.Variants != widths || widths == 0 {
			t.Errorf("the meter counts %d bytes of widths over %d built, want the encode's own sum", meter.Variants, widths)
		}
		if !meter.Unlimited || meter.Left() != -1 || meter.Full() {
			t.Errorf("an installation with no ceiling: %+v, want unlimited with no bar", meter)
		}
		return nil
	}); err != nil {
		t.Fatalf("acme's meter: %v", err)
	}
	if big.Width == 0 {
		t.Fatal("the picture the encode measured came back unmeasured")
	}

	// The same sum as a ceiling: the meter reads full, and the upload that
	// follows is refused by the number the meter just showed. Both answers come
	// out of one reader, so the only way they can disagree is a commit between
	// them, and the refusal is the one that decides.
	roomy := int64(1<<20) + big.Size
	var used, built int64
	if err := db.Run(tenancy.WithTenant(t.Context(), globex), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		globexMeter, err := internal.NewService(store, 16<<20, roomy, 0).Quota(ctx, tx)
		if err != nil {
			return err
		}
		used, built = globexMeter.Used, globexMeter.Variants
		return nil
	}); err != nil {
		t.Fatalf("globex's meter: %v", err)
	}
	if used <= 0 || built != 0 {
		t.Errorf("globex, which ran no encode: used %d, widths %d, want its own bytes and no widths", used, built)
	}

	// A caption, its revision guard, and the outbox row that goes with it.
	var shown *contracts.File
	err = db.Run(tenancy.WithActor(tenancy.WithTenant(t.Context(), acme), uuid.New()), conn,
		func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			before := len(published(t, tx))
			written, err := svc.SetAlt(ctx, tx, acmePicture.ID, "  The cover, from the roof  ", false, acmePicture.UpdatedAt)
			if err != nil {
				return err
			}
			shown = written
			if written.Alt != "The cover, from the roof" {
				t.Errorf("the caption is %q, want the words with their padding removed", written.Alt)
			}
			if got := published(t, tx)[before:]; len(got) != 1 || got[0] != contracts.EventAltered {
				t.Errorf("captioning published %v, want one %s", got, contracts.EventAltered)
			}
			// The instant this caller was shown is now the past: the row moved by
			// this write, and a second caption carrying the old one is refused
			// rather than overwriting the sentence that just landed.
			if _, err := svc.SetAlt(ctx, tx, acmePicture.ID, "somebody else's", false, acmePicture.UpdatedAt); !errors.Is(err, crud.ErrConflict) {
				t.Errorf("a caption against the instant the row has passed = %v, want ErrConflict", err)
			}
			// The refused write reaches no further than its own refusal: the row
			// still carries the accepted caption, at the same instant.
			still, err := crud.Get[*contracts.File](tx, acmePicture.ID)
			if err != nil {
				return err
			}
			if still.Alt != shown.Alt || !still.UpdatedAt.Equal(shown.UpdatedAt) {
				t.Errorf("the refused caption left alt %q at %s, want %q untouched", still.Alt, still.UpdatedAt, shown.Alt)
			}
			return nil
		})
	if err != nil {
		t.Fatalf("acme's caption: %v", err)
	}

	// The column's own CHECK answers a write that came by no command at all: the
	// caption and the claim that there is nothing to say do not both go in, and
	// the database says so even to a statement that never passed the entity.
	err = db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Exec(`UPDATE files SET decorative = true WHERE id = ?`, acmePicture.ID).Error
	})
	if err == nil || !strings.Contains(err.Error(), "file_files_decorative") {
		t.Errorf("words beside a decorative claim: %v, want the CHECK to refuse them", err)
	}
}
