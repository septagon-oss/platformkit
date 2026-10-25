package internal_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/porttest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	picture "github.com/septagon-oss/platformkit/modules/picture/contracts"
	"github.com/septagon-oss/platformkit/modules/picture/contracts/picturetest"
	"github.com/septagon-oss/platformkit/modules/saved"
	"github.com/septagon-oss/platformkit/modules/saved/contracts"
	"github.com/septagon-oss/platformkit/modules/saved/contracts/savedtest"
	"github.com/septagon-oss/platformkit/modules/saved/internal"
)

// TestSQLServiceConforms runs saved's own contract suite — the same cases the
// memory fake answers — against the real table, in a real tenant transaction,
// under row-level security. Where only a transaction can show something, the
// case shows it here: a refused save writes nothing because the transaction that
// asked for it is the one that carries the write.
func TestSQLServiceConforms(t *testing.T) {
	savedtest.RunService(t, world)
}

// desk is the fixture's authority owner: it answers who is acting and what they
// hold right now, from the transaction it is handed. A test revokes through it
// mid-call, which is the only way to ask whether a command trusts a grant it read
// before the wait or the one that survives it.
type desk struct{ held map[uuid.UUID][]string }

func (d *desk) grant(person uuid.UUID, permissions ...string) {
	d.held[person] = permissions
}

func (d *desk) drop(person uuid.UUID) { d.held[person] = nil }

func (d *desk) Current(ctx context.Context, _ db.Tx[db.Tenant], permission string) (string, error) {
	tenant, ok := tenancy.FromContext(ctx)
	if !ok || tenant.ID == uuid.Nil {
		return "", db.ErrNoTenant
	}
	principal, signed := tenancy.PrincipalFrom(ctx)
	if !signed || principal.UserID == uuid.Nil {
		return "", contracts.ErrDenied
	}
	for _, held := range d.held[principal.UserID] {
		if held == permission {
			return principal.UserID.String(), nil
		}
	}
	return "", contracts.ErrDenied
}

// world gives one case a schema of its own: its own foundation, its own
// saved_items table, its own outbox, and nothing another case wrote.
func world(t *testing.T, run func(savedtest.Fixture)) {
	t.Helper()
	_, conn := dbtest.Schema(t, saved.Migrations)

	source := &savedtest.Source{
		Value: contracts.Capture{Title: "The first thing", Locale: "en-GB", Version: "v1",
			Values: json.RawMessage(`{"size":"red","fit":"relaxed"}`)},
		Destination: "/app/example/things/one",
	}
	grants := &desk{held: map[uuid.UUID][]string{}}
	owner, other, stranger := uuid.New(), uuid.New(), uuid.New()
	grants.grant(owner, contracts.PermissionRead, contracts.PermissionWrite)
	grants.grant(other, contracts.PermissionRead, contracts.PermissionWrite)
	home, abroad := tenancy.Tenant{ID: uuid.New(), Slug: "acme"}, tenancy.Tenant{ID: uuid.New(), Slug: "away"}
	// The fixture's clock is the one this adapter stamps with, which is the point of
	// the case that advances it: a copy's instant is the composition's fact, and an
	// adapter that read the process clock instead would answer the fake's suite with
	// a time the suite never set.
	clock := porttest.NewClock(time.Now().UTC())
	service := internal.NewService(grants, []contracts.Savable{
		{Module: "example", Kind: "thing", Words: words("Save thing", "Saved to things"), Source: source},
		{Module: "example", Kind: "plan", Words: words("Save plan", "Saved to plans"), Source: source},
	})
	service.Clock = clock.Now
	undeclared := internal.NewService(grants, nil)
	undeclared.Clock = clock.Now

	signIn := func(tenant tenancy.Tenant, person uuid.UUID) context.Context {
		return tenancy.WithPrincipal(tenancy.WithTenant(t.Context(), tenant), tenancy.Principal{UserID: person})
	}
	homeOf := signIn(home, owner)

	run(savedtest.Fixture{
		Owner: homeOf, OtherPerson: signIn(home, other),
		OtherTenant: signIn(abroad, owner), Denied: signIn(home, stranger),
		Service: service, Undeclared: undeclared,
		Within: func(ctx context.Context, body func(db.Tx[db.Tenant]) error) error {
			return db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error { return body(tx) })
		},
		// The witnesses read through the owner's own tenant transaction, which is
		// the scope the contract is written in: what the service cannot see, an
		// ordinary caller cannot see either. The tenant boundary beneath this one
		// is migrations/rls_test.go's walk, which migrates this table too.
		Rows:      func() string { return witness(t, conn, homeOf) },
		Published: func() []string { return published(t, conn, homeOf, home) },
		Source:    source,
		Clock:     clock,
		Revoke:    func() { grants.drop(owner) },
		// The retention case's composition comes from this world, not from the
		// suite: the bytes are the picture module's own fixture.
		Picture:         retainedPicture(t),
		ComposedPicture: picturetest.Oracle,
	})
}

func words(save, savedTo string) contracts.Words {
	return contracts.Words{Save: save, Saved: savedTo, Remove: "Remove", ListTitle: "My things"}
}

// retainedPicture compiles the picture the suite's retention case saves and reads
// back. The fixture bytes belong to the picture module, and shipped source may not
// import that helper, so the world that runs the suite brings the composition.
func retainedPicture(t *testing.T) contracts.PictureInput {
	t.Helper()
	template, err := picture.Compile("fixture", picturetest.SVG)
	if err != nil {
		t.Fatal(err)
	}
	return contracts.PictureInput{Template: template, Value: picturetest.Value()}
}

// witness renders every row this tenant can see, so a case can say "nothing moved"
// about a refusal rather than about the one field it happened to look at.
func witness(t *testing.T, conn *db.Conn, ctx context.Context) string {
	t.Helper()
	var out string
	err := db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw(`SELECT coalesce(jsonb_agg(to_jsonb(saved_items) ORDER BY id), '[]')::text FROM saved_items`).Scan(&out).Error
	})
	if err != nil {
		t.Fatalf("witness the rows: %v", err)
	}
	return out
}

func published(t *testing.T, conn *db.Conn, ctx context.Context, tenant tenancy.Tenant) []string {
	t.Helper()
	var names []string
	err := db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw(`SELECT name FROM platformkit_outbox WHERE tenant_id = ? ORDER BY created_at, id`, tenant.ID).Scan(&names).Error
	})
	if err != nil {
		t.Fatalf("read the outbox: %v", err)
	}
	return names
}
