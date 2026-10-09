package internal_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/personaldata"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/subject/contracts"
	"github.com/septagon-oss/platformkit/modules/subject/internal"
	"github.com/septagon-oss/platformkit/modules/user"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// The one composition this module has is a list of parts, so the list is what these
// cases are built on: two owners, named "beta" and "alpha" on purpose so the order
// the answer arrives in is the order somebody wrote them in and not the order a
// resolver happened to build them in.
//
// What they cannot do is reach a database — the fakes own no rows — so each case
// asks the question the fakes can answer truthfully: who was asked, in what order,
// with what in their hand, and what the transaction ended up holding. Which owner
// erases which column, and whether it really stops naming the person, is the owners'
// own test, in each of their packages (modules/user, modules/auth, …) and not
// something a fake here could certify.

var acme = tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}

// recorder is the parts' shared log: an order, and the subject each was handed.
type recorder struct {
	asks []string
	seen []personaldata.Subject
}

type ownerStub struct {
	name string
	rec  *recorder
	err  error
}

func (o ownerStub) Export(_ context.Context, _ db.Tx[db.Tenant], s personaldata.Subject) (json.RawMessage, error) {
	o.rec.asks = append(o.rec.asks, "export "+o.name)
	o.rec.seen = append(o.rec.seen, s)
	return json.Marshal(map[string]string{"module": o.name, "email": s.Email})
}

func (o ownerStub) Erase(_ context.Context, _ db.Tx[db.Tenant], s personaldata.Subject) error {
	o.rec.asks = append(o.rec.asks, "erase "+o.name)
	o.rec.seen = append(o.rec.seen, s)
	return o.err
}

// peopleStub is the one collaborator: a reader of the person's own row. It reads the
// real users table rather than remembering an argument, because "the strings come
// from the row and not from the caller" is the claim these cases are checking.
type peopleStub struct {
	usercontracts.Service
}

func (peopleStub) Get(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*usercontracts.User, error) {
	var u usercontracts.User
	err := tx.DB().WithContext(ctx).Table("users").
		Select("id, email, display_name, handle").Where("id = ?", id).First(&u).Error
	if err != nil {
		return nil, crud.ErrNotFound
	}
	return &u, nil
}

func seed(t *testing.T, conn *db.Conn, s personaldata.Subject) {
	t.Helper()
	in := tenancy.WithTenant(t.Context(), acme)
	err := db.Run(in, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Exec(`INSERT INTO users (id, tenant_id, email, display_name, handle) VALUES (?, ?, ?, ?, ?)`,
			s.ID, acme.ID, s.Email, s.DisplayName, s.Handle).Error
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
}

func service(rec *recorder, owners ...ownerStub) contracts.Service {
	parts := make([]personaldata.Part, 0, len(owners))
	for _, o := range owners {
		parts = append(parts, personaldata.Part{Module: o.name, Data: o})
	}
	return internal.NewService(parts, peopleStub{}, func() time.Time {
		return time.Date(2026, time.October, 9, 12, 0, 0, 0, time.UTC)
	})
}

func transact(t *testing.T, conn *db.Conn, fn func(context.Context, db.Tx[db.Tenant]) error) error {
	t.Helper()
	return db.Run(tenancy.WithTenant(t.Context(), acme), conn, fn)
}

func TestExportingAPersonBundlesEveryOwnerInTheOrderSomebodyWroteThem(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations)
	events.Declare[contracts.Erased](contracts.EventErased)
	subject := personaldata.Subject{ID: uuid.New(), Email: "Jo.Jones@Example.com", DisplayName: "Jo Jones", Handle: "jjones"}
	seed(t, conn, subject)
	rec := &recorder{}
	svc := service(rec,
		ownerStub{name: "beta", rec: rec},
		ownerStub{name: "alpha", rec: rec})

	var first, second *contracts.Bundle
	if err := transact(t, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		first, err = svc.Export(ctx, tx, subject.ID)
		return err
	}); err != nil {
		t.Fatalf("export: %v", err)
	}
	// Snapshot the asks before the second export, which asks the same two owners
	// again and would bury the order this case is about.
	asks := slices.Clone(rec.asks)
	if err := transact(t, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		second, err = svc.Export(ctx, tx, subject.ID)
		return err
	}); err != nil {
		t.Fatalf("the second export: %v", err)
	}

	if !slices.Equal(asks, []string{"export alpha", "export beta"}) {
		t.Errorf("the owners were asked %v; a bundle is read as the whole person, so the order must be one somebody wrote down", asks)
	}
	for _, s := range rec.seen {
		if s.Email != subject.Email || s.DisplayName != subject.DisplayName || s.Handle != subject.Handle {
			t.Errorf("the contributor was handed %+v and not the person's own strings", s)
		}
	}
	if !bytes.Equal(first.Zip, second.Zip) || first.Digest != second.Digest {
		t.Error("two exports of the same rows are not the same bytes, so a bundle cannot be digested and compared")
	}

	zr, err := zip.NewReader(bytes.NewReader(first.Zip), int64(len(first.Zip)))
	if err != nil {
		t.Fatalf("the bundle is not a zip: %v", err)
	}
	var entries []string
	bodies := map[string]string{}
	for _, f := range zr.File {
		entries = append(entries, f.Name)
		r, err := f.Open()
		if err != nil {
			t.Fatalf("open %s: %v", f.Name, err)
		}
		var buf bytes.Buffer
		if _, err := buf.ReadFrom(r); err != nil {
			t.Fatalf("read %s: %v", f.Name, err)
		}
		r.Close()
		bodies[f.Name] = buf.String()
	}
	want := []string{"alpha.json", "beta.json", "manifest.json"}
	if !slices.Equal(entries, want) {
		t.Errorf("the bundle holds %v, want %v", entries, want)
	}
	for _, name := range want {
		if bodies[name] == "" {
			t.Errorf("%s is empty", name)
		}
	}
	// The manifest is the answer to "what is in here and what is missing", so it
	// has to name the tenant the transaction was opened against, the person, and
	// every part with its digest.
	var manifest contracts.Manifest
	if err := json.Unmarshal([]byte(bodies["manifest.json"]), &manifest); err != nil {
		t.Fatalf("manifest.json: %v (%s)", err, bodies["manifest.json"])
	}
	if manifest.Tenant != acme.ID || manifest.Subject != subject.ID {
		t.Errorf("the manifest names tenant %s and subject %s", manifest.Tenant, manifest.Subject)
	}
	if len(manifest.Parts) != 2 || manifest.Parts[0].Module != "alpha" || manifest.Parts[1].Module != "beta" {
		t.Errorf("the manifest's parts are %+v", manifest.Parts)
	}
	if manifest.Parts[0].Digest != digestOf([]byte(bodies["alpha.json"])) {
		t.Errorf("the manifest's digest for alpha is not the entry's bytes")
	}
}

func TestErasingAPersonAsksEveryOwnerOnceAndPublishesWithTheRows(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations)
	events.Declare[contracts.Erased](contracts.EventErased)
	subject := personaldata.Subject{ID: uuid.New(), Email: "jo.jones@example.com", DisplayName: "Jo Jones", Handle: "jjones"}
	seed(t, conn, subject)
	rec := &recorder{}
	svc := service(rec,
		ownerStub{name: "beta", rec: rec},
		ownerStub{name: "alpha", rec: rec})

	var receipt *contracts.Erasure
	if err := transact(t, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		receipt, err = svc.Erase(ctx, tx, contracts.EraseInput{Subject: subject.ID, Confirm: "JO.JONES@EXAMPLE.COM"})
		return err
	}); err != nil {
		t.Fatalf("erase: %v", err)
	}
	if got := rec.asks; !slices.Equal(got, []string{"erase alpha", "erase beta"}) {
		t.Errorf("the owners were asked %v", got)
	}
	if !slices.Equal(receipt.Modules, []string{"alpha", "beta"}) || receipt.AlreadyErased {
		t.Errorf("the receipt is %+v", receipt)
	}
	if !receipt.At.Equal(time.Date(2026, time.October, 9, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("the receipt is stamped %s, not the clock this service was built with", receipt.At)
	}
	if n := published(t, conn, contracts.EventErased); n != 1 {
		t.Errorf("%d %s events published; an irreversible act has to leave the record that it happened", n, contracts.EventErased)
	}
}

func TestAnErasureWhoseConfirmationIsNotThePersonsAddressWritesNothing(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations)
	events.Declare[contracts.Erased](contracts.EventErased)
	subject := personaldata.Subject{ID: uuid.New(), Email: "jo.jones@example.com", DisplayName: "Jo Jones", Handle: "jjones"}
	seed(t, conn, subject)
	rec := &recorder{}
	svc := service(rec, ownerStub{name: "alpha", rec: rec})

	err := transact(t, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := svc.Erase(ctx, tx, contracts.EraseInput{Subject: subject.ID, Confirm: "jo.jones@exampl.com"})
		return err
	})
	if !errors.Is(err, crud.ErrInvalid) {
		t.Fatalf("the erasure = %v, want ErrInvalid", err)
	}
	if len(rec.asks) != 0 {
		t.Errorf("a refused erasure still asked the owners: %v", rec.asks)
	}
	if n := published(t, conn, contracts.EventErased); n != 0 {
		t.Errorf("%d events published by a refused erasure", n)
	}
}

func TestTheSecondErasureOfTheSamePersonAnswersTheFirstReceipt(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations)
	events.Declare[contracts.Erased](contracts.EventErased)
	subject := personaldata.Subject{ID: uuid.New(), Email: "jo.jones@example.com", DisplayName: "Jo Jones", Handle: "jjones"}
	seed(t, conn, subject)
	rec := &recorder{}
	svc := service(rec, ownerStub{name: "alpha", rec: rec})
	erased := personaldata.Subject{ID: subject.ID}.ErasedEmail()

	if err := transact(t, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Exec(`UPDATE users SET email = ?, display_name = '', handle = NULL WHERE id = ?`,
			erased, subject.ID).Error
	}); err != nil {
		t.Fatalf("the first erasure: %v", err)
	}
	rec.asks = nil

	var receipt *contracts.Erasure
	if err := transact(t, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		receipt, err = svc.Erase(ctx, tx, contracts.EraseInput{Subject: subject.ID, Confirm: erased})
		return err
	}); err != nil {
		t.Fatalf("the second erasure = %v, want the first one's receipt", err)
	}
	if !receipt.AlreadyErased {
		t.Errorf("the second erasure reports %+v", receipt)
	}
	if len(rec.asks) != 0 {
		t.Errorf("an already-erased person was scrubbed again: %v", rec.asks)
	}
	if n := published(t, conn, contracts.EventErased); n != 0 {
		t.Errorf("%d events published by an erasure that ran nothing", n)
	}
}

func TestAnOwnerThatRefusesEndsTheErasureWithNothingPublished(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations)
	events.Declare[contracts.Erased](contracts.EventErased)
	subject := personaldata.Subject{ID: uuid.New(), Email: "jo.jones@example.com", DisplayName: "Jo Jones", Handle: "jjones"}
	seed(t, conn, subject)
	rec := &recorder{}
	svc := service(rec,
		ownerStub{name: "alpha", rec: rec},
		ownerStub{name: "beta", rec: rec, err: crud.ErrInvalid},
		ownerStub{name: "gamma", rec: rec})

	err := transact(t, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := svc.Erase(ctx, tx, contracts.EraseInput{Subject: subject.ID, Confirm: subject.Email})
		return err
	})
	if !errors.Is(err, crud.ErrInvalid) {
		t.Fatalf("the erasure = %v, want the refusing owner's error", err)
	}
	if !slices.Equal(rec.asks, []string{"erase alpha", "erase beta"}) {
		t.Errorf("the owners were asked %v; the one after the refusal must not be asked", rec.asks)
	}
	if n := published(t, conn, contracts.EventErased); n != 0 {
		t.Errorf("%d events published by an erasure that failed: a half-erased person is not a person who is erased", n)
	}
}

func published(t *testing.T, conn *db.Conn, name string) int {
	t.Helper()
	var n int
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw(`SELECT count(*) FROM platformkit_outbox WHERE name = ?`, name).Scan(&n).Error
	})
	if err != nil {
		t.Fatalf("reading the outbox: %v", err)
	}
	return n
}

func digestOf(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}
