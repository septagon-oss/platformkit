package internal_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/personaldata"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	"github.com/septagon-oss/platformkit/modules/auth/internal"
	notificationmodule "github.com/septagon-oss/platformkit/modules/notification"
	usermodule "github.com/septagon-oss/platformkit/modules/user"
)

// TestNoDispatchRowNamesAPlaintextCredential runs the module's own scan — every table in
// this schema, enumerated from the database rather than named here — over a send that
// really happened, and then asks the sharper question the scan cannot ask.
//
// A scan for the plaintext can only notice a table that stored the plaintext. It says
// nothing about a table that stored something else about the credential, so the row's one
// credential-derived value is read as bytes and compared to sha256 of the token: that is
// the whole of what this table may know about a link.
func TestNoDispatchRowNamesAPlaintextCredential(t *testing.T) {
	admin, conn := dbtest.Schema(t, usermodule.Migrations, notificationmodule.Migrations, auth.Migrations)
	box := &authtest.Mailbox{}
	users := realUsers()
	svc := internal.NewService(users, nil, mailedDelivery(box))
	seed(t, conn, acme)
	person := invitedPerson(t, conn, users, "dispatch-plaintext@acme.example.com")
	ctx := httpx.WithConn(tenancy.WithTenant(t.Context(), acme), conn)
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return svc.Offer(ctx, tx, person)
	}); err != nil {
		t.Fatal(err)
	}
	token := authtest.TokenIn(box.Sent()[0].Body)
	if token == "" {
		t.Fatal("no credential left, so the scan below would scan nothing")
	}
	refuseNothingYet(t, admin, token)

	var stored []byte
	row(t, admin, "SELECT token_hash FROM auth_mail_dispatch").Scan(&stored)
	if want := contracts.Hash(token); !bytes.Equal(stored, []byte(want)) {
		t.Errorf("the dispatch row holds %x, want sha256 of the credential and nothing else", stored)
	}
}

// TestAnotherTenantsDispatchRowIsNeitherReadableNorWritable is the tenancy claim over the
// new table, asked of the application role rather than of the administrator that seeded the
// row: the other tenant's transaction reads none, and its INSERT naming this tenant's
// credential, person and address is refused by the policy's WITH CHECK half before it can
// put a row that names them in the other tenant's scope.
func TestAnotherTenantsDispatchRowIsNeitherReadableNorWritable(t *testing.T) {
	admin, conn := dbtest.Schema(t, usermodule.Migrations, notificationmodule.Migrations, auth.Migrations)
	box := &authtest.Mailbox{}
	users := realUsers()
	svc := internal.NewService(users, nil, mailedDelivery(box))
	seed(t, conn, acme)
	seed(t, conn, globex)
	const address = "dispatch-tenancy@acme.example.com"
	person := invitedPerson(t, conn, users, address)
	ctx := httpx.WithConn(tenancy.WithTenant(t.Context(), acme), conn)
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return svc.Offer(ctx, tx, person)
	}); err != nil {
		t.Fatal(err)
	}
	digest := contracts.Hash(authtest.TokenIn(box.Sent()[0].Body))

	foreign := tenancy.WithTenant(httpx.WithConn(t.Context(), conn), globex)
	err := db.Run(foreign, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var seen int
		if err := tx.DB().Raw("SELECT count(*) FROM auth_mail_dispatch").Row().Scan(&seen); err != nil {
			return err
		}
		if seen != 0 {
			t.Errorf("the other tenant reads %d dispatch rows, want none", seen)
		}
		return tx.DB().Exec("INSERT INTO auth_mail_dispatch (tenant_id, user_id, token_hash, kind, recipient, state)"+
			" VALUES (?, ?, ?, ?, ?, 'unsent')", acme.ID, person, digest, contracts.MailSetPassword, address).Error
	})
	if err == nil {
		t.Fatal("the other tenant wrote into this tenant's dispatch table")
	}
	// The refusal wrote nothing, and the row it named is the row that was already there:
	// unchanged, in its own tenant, at the state its own transport decided.
	state, recipient, settled := dispatchOf(t, admin, digest)
	if state != "sent" || recipient != address || !settled.Valid {
		t.Errorf("the row the foreign write named now reads %q/%q settled=%v, want it untouched", state, recipient, settled)
	}
}

// TestEveryDispatchRowNamesTheCredentialItsMessageWasDecidedFor is the table's whole
// reason in one sweep over it: one row for every message this module decided to send, no
// row for the branch that decided to send none, and no row for a credential this run never
// minted. The last clause is what catches a table that grows from somewhere nobody named.
func TestEveryDispatchRowNamesTheCredentialItsMessageWasDecidedFor(t *testing.T) {
	admin, conn := dbtest.Schema(t, usermodule.Migrations, notificationmodule.Migrations, auth.Migrations)
	mailer := &refusingDispatchMailer{}
	users := realUsers()
	send := func(address string) *refusingDispatchMailer {
		t.Helper()
		svc := internal.NewService(users, nil, internal.Delivery{
			Mailer: mailer, MailDelivery: mailer, Hosts: authtest.Host("acme.example.com"), Secure: true,
		})
		person := invitedPerson(t, conn, users, address)
		if err := db.Run(httpx.WithConn(tenancy.WithTenant(t.Context(), acme), conn), conn,
			func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				return svc.Offer(ctx, tx, person)
			}); err != nil {
			t.Fatalf("offer to %s: %v", address, err)
		}
		return mailer
	}
	seed(t, conn, acme)

	minted := map[string]bool{}
	if token := send("dispatch-property-sent@acme.example.com").token; token != "" {
		minted[hex.EncodeToString(contracts.Hash(token))] = true
	}
	mailer.refuse = true
	if token := send("dispatch-property-refused@acme.example.com").token; token != "" {
		minted[hex.EncodeToString(contracts.Hash(token))] = true
	}
	// The composition with no transport: the person is answered as though the mail had
	// gone, the ledger says nothing went, and no credential and no dispatch row exists — a
	// link nobody is sent is a live door in a table for nothing.
	quiet := internal.NewService(users, nil, internal.Delivery{})
	quietPerson := invitedPerson(t, conn, users, "dispatch-property-none@acme.example.com")
	if err := db.Run(httpx.WithConn(tenancy.WithTenant(t.Context(), acme), conn), conn,
		func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			return quiet.Offer(ctx, tx, quietPerson)
		}); err != nil {
		t.Fatalf("offer with no mailer: %v", err)
	}

	rows, err := admin.Query("SELECT encode(token_hash, 'hex'), state FROM auth_mail_dispatch ORDER BY created_at")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var seen []string
	for rows.Next() {
		var hash, state string
		if err := rows.Scan(&hash, &state); err != nil {
			t.Fatal(err)
		}
		if !minted[hash] {
			t.Errorf("a dispatch row names %s, which no credential this run minted hashes to", hash)
		}
		seen = append(seen, state)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(seen) != len(minted) {
		t.Errorf("dispatch rows = %v for %d decided messages, want one each", seen, len(minted))
	}
	var tokens, credentials int
	row(t, admin, "SELECT count(*) FROM password_tokens WHERE user_id = $1", quietPerson).Scan(&tokens)
	row(t, admin, "SELECT count(*) FROM auth_mail_dispatch WHERE user_id = $1", quietPerson).Scan(&credentials)
	if tokens != 0 || credentials != 0 {
		t.Errorf("the composition with no mailer left %d credentials and %d dispatch rows, want neither",
			tokens, credentials)
	}
}

// TestThePersonExportNamesTheLinkItDecidedForThemAndTheErasureRemovesIt is this table's
// answer to the two requests a person can make about their own data: the export says a link
// was decided for them, of which kind, and whether anyone can say it left — and carries no
// copy of a door. The erasure removes the row outright, because a credential event is not
// something the person did, and reaches nobody else's rows on the way.
func TestThePersonExportNamesTheLinkItDecidedForThemAndTheErasureRemovesIt(t *testing.T) {
	admin, conn := dbtest.Schema(t, usermodule.Migrations, notificationmodule.Migrations, auth.Migrations)
	box := &authtest.Mailbox{}
	users := realUsers()
	svc := internal.NewService(users, nil, mailedDelivery(box))
	seed(t, conn, acme)
	const address = "dispatch-export@acme.example.com"
	const other = "dispatch-export-colleague@acme.example.com"
	person := invitedPerson(t, conn, users, address)
	colleague := invitedPerson(t, conn, users, other)
	ctx := httpx.WithConn(tenancy.WithTenant(t.Context(), acme), conn)
	for _, id := range []uuid.UUID{person, colleague} {
		if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			return svc.Offer(ctx, tx, id)
		}); err != nil {
			t.Fatal(err)
		}
	}

	var part json.RawMessage
	err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		part, err = internal.PersonalData().Data.Export(ctx, tx, personaldata.Subject{ID: person, Email: address})
		return err
	})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	var bundle struct {
		MailDispatch []struct {
			Kind      string `json:"kind"`
			CreatedAt string `json:"createdAt"`
		} `json:"mailDispatch"`
	}
	if err := json.Unmarshal(part, &bundle); err != nil {
		t.Fatalf("read the exported part: %v", err)
	}
	if len(bundle.MailDispatch) != 1 || bundle.MailDispatch[0].Kind != contracts.MailSetPassword {
		t.Errorf("the export names %v, want the one set-password link decided for this person", bundle.MailDispatch)
	}
	if bundle.MailDispatch[0].CreatedAt == "" {
		t.Error("the exported dispatch row carries no date, so it answers no question")
	}
	for _, letter := range box.Sent() {
		if token := authtest.TokenIn(letter.Body); token != "" && bytes.Contains(part, []byte(token)) {
			t.Error("the export carried the credential a link was built from")
		}
		if hash := contracts.Hash(authtest.TokenIn(letter.Body)); len(hash) > 0 && bytes.Contains(part, hash) {
			t.Error("the export carried a credential hash, which is a copy of a door")
		}
	}

	err = db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return internal.PersonalData().Data.Erase(ctx, tx, personaldata.Subject{ID: person, Email: address})
	})
	if err != nil {
		t.Fatalf("erase: %v", err)
	}
	var mine, theirs int
	row(t, admin, "SELECT count(*) FROM auth_mail_dispatch WHERE user_id = $1", person).Scan(&mine)
	row(t, admin, "SELECT count(*) FROM auth_mail_dispatch WHERE user_id = $1", colleague).Scan(&theirs)
	if mine != 0 || theirs != 1 {
		t.Errorf("the erasure left %d of this person's rows and %d of a colleague's, want 0 and 1", mine, theirs)
	}
}
