package main

// An erasure ends what the tenant holds about a person, including the copy this
// composition made of them a moment earlier: the zip the export walk stored is one
// person's rows from every owner, and after the erasure neither the link the person
// already minted nor a link minted by an officer may hand those rows out.

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	subjectcontracts "github.com/septagon-oss/platformkit/modules/subject/contracts"
	"github.com/septagon-oss/platformkit/pkit"
)

func TestAnErasedPersonsBundleIsNotHandedOutAfterTheErasure(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)
	svc, ok := pkit.Value[subjectcontracts.Service](composeReference(cfg, pkit.Development).plan())
	if !ok || svc == nil {
		t.Fatal("the composition hands out no subjectcontracts.Service")
	}

	acme := acmeTenant(t, cfg)
	email := "erased.persona@acme.localhost"
	provisionAs(t, cfg, c, acme, email, "member")
	person := signIn(t, cfg, acmeHost, email, adminPass)

	code, body := do(t, cfg, person, http.MethodPost, acmeHost, exportsPath, `{}`)
	if code < 200 || code > 299 {
		t.Fatalf("asking for a copy of one's own data = %d %s", code, body)
	}
	request, subject := field(t, body, "id"), field(t, body, "subject")

	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer conn.Close()
	for range 2 {
		if code, body := do(t, cfg, person, http.MethodGet, acmeHost, exportsPath+"/"+request, ""); code == http.StatusOK && field(t, body, "state") == "ready" {
			break
		}
		runExportWalk(t, c, conn)
	}
	code, body = do(t, cfg, person, http.MethodPost, acmeHost, exportsPath+"/"+request+"/link", "")
	if code != http.StatusOK {
		t.Fatalf("minting the person's own link before the erasure = %d %s", code, body)
	}
	before := field(t, body, "token")
	// A second link, opened now, says what a bundle that names the person looks like at the door.
	code, body = do(t, cfg, person, http.MethodPost, acmeHost, exportsPath+"/"+request+"/link", "")
	if code != http.StatusOK {
		t.Fatalf("minting a second link before the erasure = %d %s", code, body)
	}
	if code, zip := do(t, cfg, nil, http.MethodGet, acmeHost, "/api/v1/public/subject/bundles/"+field(t, body, "token"), ""); code != http.StatusOK || !strings.Contains(zip, email) {
		t.Fatalf("before the erasure the bundle answered %d and does not carry %s; this case reads the address inside the zip", code, email)
	}

	var tenant tenancy.Tenant
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		var e error
		tenant, e = c.testTenants().ByHost(ctx, tx, acmeHost)
		return e
	}); err != nil {
		t.Fatalf("which tenant is acme: %v", err)
	}
	officer := uuid.New()
	in := tenancy.WithActor(tenancy.WithTenant(t.Context(), tenant), officer)
	if err := db.Run(in, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, _, err := svc.RequestErase(ctx, tx, subjectcontracts.EraseInput{Subject: uuid.MustParse(subject), Confirm: email})
		return err
	}); err != nil {
		t.Fatalf("erasing the person: %v", err)
	}

	// The link minted before the erasure: refused, or a bundle that no longer names them.
	if code, zip := do(t, cfg, nil, http.MethodGet, acmeHost, "/api/v1/public/subject/bundles/"+before, ""); code == http.StatusOK && strings.Contains(zip, email) {
		t.Errorf("the link minted before the erasure still hands out a bundle carrying %s", email)
	}

	// An officer's link onto the same request, minted after the erasure.
	var after *subjectcontracts.Link
	if err := db.Run(in, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		after, err = svc.Mint(ctx, tx, uuid.MustParse(request), officer, true)
		return err
	}); err == nil {
		if code, zip := do(t, cfg, nil, http.MethodGet, acmeHost, "/api/v1/public/subject/bundles/"+after.Token, ""); code == http.StatusOK && strings.Contains(zip, email) {
			t.Errorf("an officer minted a link after the erasure and it hands out a bundle carrying %s", email)
		}
	}
}
