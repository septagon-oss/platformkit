package main

// The brief's own acceptance line, asked where every module is composed:
// "a test exports a seeded person and finds their rows from each module".
//
// It could not be asked inside modules/subject, and that is not an accident of
// layout: house rule 3 forbids one module reading another's tables, so the one
// place a person's rows can all be put in front of one command is the composition
// that names all of them. The orchestrator's own cases
// (modules/subject/internal/service_test.go) ask who was called, in what order and
// with what published, over owners that own nothing; this one asks whether the rows
// that are really in a real tenant's tables come back in the bundle and stop naming
// the person afterwards. The defect this shape exists for — three contributors naming
// columns their own migrations never made — was invisible from both sides of that
// split, and this is the shape that cannot miss it again.
//
// The person is seeded by hand rather than through the doors: an invited user, a
// task assigned to them whose description names them, a page they authored, and a
// notice another person received about them. The last of those four is the one that
// makes the test worth its minutes — an erasure that limited itself to rows carrying
// the recipient's own id would pass any test that seeded only those.

import (
	"archive/zip"
	"bytes"
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	subjectcontracts "github.com/septagon-oss/platformkit/modules/subject/contracts"
	"github.com/septagon-oss/platformkit/pkit"
)

func TestExportingASeededPersonFindsTheirRowsFromEveryModuleThatHoldsThem(t *testing.T) {
	const (
		email  = "Jo.Jones@Example.com"
		name   = "Jo Jones"
		handle = "jjones"
	)
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	svc, ok := pkit.Value[subjectcontracts.Service](composeReference(cfg, pkit.Development).plan())
	if !ok || svc == nil {
		t.Fatal("the composition hands out no subjectcontracts.Service, so nobody can answer for a person")
	}

	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	var tenant tenancy.Tenant
	err = dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		var e error
		tenant, e = c.testTenants().ByHost(ctx, tx, acmeHost)
		return e
	})
	if err != nil {
		t.Fatalf("which tenant is acme: %v", err)
	}
	in := tenancy.WithTenant(t.Context(), tenant)

	person, task, page := uuid.New(), uuid.New(), uuid.New()
	notice, colleagueNotice := uuid.New(), uuid.New()
	seed := func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		for _, q := range []struct {
			sql  string
			args []any
		}{
			{`INSERT INTO users (id, tenant_id, email, display_name, handle) VALUES (?, ?, ?, ?, ?)`,
				[]any{person, tenant.ID, email, name, handle}},
			// A work record somebody else owns, whose free text names the person twice.
			{`INSERT INTO tasks (id, tenant_id, title, description, assignee_id) VALUES (?, ?, ?, ?, ?)`,
				[]any{task, tenant.ID, "Rewrite the register", "Ask " + name + " (" + email + ") for the old keys", person}},
			// A page they wrote themselves.
			{`INSERT INTO contents (id, tenant_id, slug, title, body, author_id) VALUES (?, ?, ?, ?, ?, ?)`,
				[]any{page, tenant.ID, "handover", "Handover notes", "Notes from " + name + ".", person}},
			// A notice the person was sent about themselves, which their own copy
			// holds, ...
			{`INSERT INTO notifications (id, tenant_id, recipient_id, title, body) VALUES (?, ?, ?, ?, ?)`,
				[]any{notice, tenant.ID, person, name + " left the register open",
					"Write to " + email + " if the keys are missing"}},
			// ... and the same sentence sent to a colleague, which the person's export
			// does not hold (it is not their notice) and which the erasure still has to
			// find: the row that *mentions* a person need not be addressed to them.
			{`INSERT INTO notifications (id, tenant_id, recipient_id, title, body) VALUES (?, ?, ?, ?, ?)`,
				[]any{colleagueNotice, tenant.ID, uuid.New(), name + " left the register open",
					"Write to " + email + " if the keys are missing"}},
		} {
			if err := tx.DB().Exec(q.sql, q.args...).Error; err != nil {
				return err
			}
		}
		return nil
	}
	if err := db.Run(in, conn, seed); err != nil {
		t.Fatalf("seed: %v", err)
	}

	var bundle *subjectcontracts.Bundle
	err = db.Run(in, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		bundle, err = svc.Export(ctx, tx, person)
		return err
	})
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	// Every module that was seeded has to answer with the row it was seeded with,
	// by its owners' own reading and not by this test's query.
	for _, want := range []struct{ entry, phrase string }{
		{"user.json", email},
		{"task.json", "Rewrite the register"},
		{"content.json", "handover"},
		{"notification.json", "left the register open"},
	} {
		if body := bundleEntry(t, bundle.Zip, want.entry); !bytes.Contains(body, []byte(want.phrase)) {
			t.Errorf("%s does not carry %q: %s", want.entry, want.phrase, body)
		}
	}
	if body := bundleEntry(t, bundle.Zip, "manifest.json"); !bytes.Contains(body, []byte(person.String())) {
		t.Errorf("the manifest does not name the person whose bundle this is: %s", body)
	}

	var receipt *subjectcontracts.Erasure
	err = db.Run(in, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		receipt, err = svc.Erase(ctx, tx, subjectcontracts.EraseInput{Subject: person, Confirm: email})
		return err
	})
	if err != nil {
		t.Fatalf("erase: %v", err)
	}
	if len(receipt.Modules) < 4 {
		t.Errorf("the erasure reached %v; every owner seeded above had to be asked", receipt.Modules)
	}

	var named int
	err = db.Run(in, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw(`SELECT
			  (SELECT count(*) FROM users WHERE id = ? AND (email = ? OR display_name = ? OR handle = ?))
			+ (SELECT count(*) FROM tasks WHERE id = ? AND (strpos(description, ?) > 0 OR strpos(description, ?) > 0))
			+ (SELECT count(*) FROM contents WHERE id = ? AND (strpos(title, ?) > 0 OR strpos(body, ?) > 0))
			+ (SELECT count(*) FROM notifications WHERE (id = ? OR id = ?) AND (strpos(title, ?) > 0 OR strpos(body, ?) > 0))`,
			person, email, name, handle,
			task, email, name,
			page, name, name,
			notice, colleagueNotice, name, email).Scan(&named).Error
	})
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	if named != 0 {
		t.Errorf("%d seeded rows still name the person after the erasure", named)
	}

	// The rows themselves stay: the task is somebody else's work record, the page is
	// the tenant's own site, the notice is what another person was told, and the user
	// row is what every audit row points at.
	var kept int
	err = db.Run(in, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw(`SELECT (SELECT count(*) FROM users WHERE id = ?)
			+ (SELECT count(*) FROM tasks WHERE id = ?)
			+ (SELECT count(*) FROM contents WHERE id = ?)
			+ (SELECT count(*) FROM notifications WHERE id = ?)`,
			person, task, page, notice).Scan(&kept).Error
	})
	if err != nil {
		t.Fatalf("re-read the rows: %v", err)
	}
	if kept != 4 {
		t.Errorf("an erasure deleted rows instead of anonymising them: %d of the four seeded rows are there", kept)
	}

	// And the irreversible act published: the outbox row is written by the same
	// commit as the scrub, which is what modules/audit turns into a trail row.
	var published int
	err = db.Run(in, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw(`SELECT count(*) FROM platformkit_outbox WHERE name = ? AND payload->>'subject' = ?`,
			subjectcontracts.EventErased, person.String()).Scan(&published).Error
	})
	if err != nil {
		t.Fatalf("read the outbox: %v", err)
	}
	if published != 1 {
		t.Errorf("%d %s events for that person; the erasure is irreversible and the record that it happened has to commit with it",
			published, subjectcontracts.EventErased)
	}
}

// bundleEntry reads one file out of the bundle, and refuses a bundle that does not hold it.
func bundleEntry(t *testing.T, archive []byte, name string) []byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatalf("the bundle is not a zip: %v", err)
	}
	for _, f := range zr.File {
		if f.Name != name {
			continue
		}
		r, err := f.Open()
		if err != nil {
			t.Fatalf("open %s: %v", name, err)
		}
		defer r.Close()
		var body bytes.Buffer
		if _, err := body.ReadFrom(r); err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		return body.Bytes()
	}
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	t.Fatalf("the bundle holds %v and not %s", names, name)
	return nil
}
