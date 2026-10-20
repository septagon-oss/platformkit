package internal_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/personaldata"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/task"
	"github.com/septagon-oss/platformkit/modules/task/internal"
)

// TestErasingAPersonRemovesWhatTheTaskSaysAboutThem is the brief's second acceptance
// line read at one owner's door: a person's name and address must stop being findable,
// and the work record that mentions them must survive being scrubbed.
//
// Three rows, because the rule has three shapes to answer. One task is *theirs* — the
// assignee is the person, and its title and description carry the address and the
// handle. One belongs to somebody else and merely *mentions* them, which is the case a
// WHERE on the person's id would miss and the reason the scrub runs over the tenant and
// not over the assignee. One sits in another tenant altogether, and the erasure must
// not reach it: row-level security is the bound, so an erasure asked of Acme answers
// for Acme's rows and cannot see Beta's.
func TestErasingAPersonRemovesWhatTheTaskSaysAboutThem(t *testing.T) {
	const (
		email  = "jo.jones@example.com"
		handle = "jjones"
		name   = "Jo Jones"
	)
	subject := uuid.New()
	acmeTenant := acme
	other := tenancy.Tenant{ID: uuid.New(), Slug: "beta", Name: "Beta"}
	betaTask := uuid.New()
	_, conn := dbtest.Schema(t, task.Migrations)

	seed := func(tenant tenancy.Tenant, id uuid.UUID, assignee any, title, description, resolution string) {
		err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
			return tx.DB().Exec(
				`INSERT INTO tasks (id, tenant_id, title, description, assignee_id, resolution)
				 VALUES (?, ?, ?, ?, ?, ?)`,
				id, tenant.ID, title, description, assignee, resolution).Error
		})
		if err != nil {
			t.Fatalf("seed %s: %v", title, err)
		}
	}
	seed(acmeTenant, uuid.New(), subject, handle+" wants a refund", email+" says the invoice was wrong", "")
	seed(acmeTenant, uuid.New(), uuid.New(), "escalation", "the client is unhappy", "asked "+email+" to review")
	seed(other, betaTask, uuid.New(), "beta's own", "mentions "+name, "cc "+email)

	subjectRow := personaldata.Subject{ID: subject, Email: email, DisplayName: name, Handle: handle}

	// Export first: it is a read, and it has to find the person's own task by its title
	// before any scrub has run over it.
	var part json.RawMessage
	err := db.Run(tenancy.WithTenant(t.Context(), acmeTenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		part, err = internal.PersonalData().Data.Export(ctx, tx, subjectRow)
		return err
	})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if !strings.Contains(string(part), handle) {
		t.Errorf("the part does not name the task the person owns: %s", part)
	}
	if strings.Contains(string(part), betaTask.String()) {
		t.Errorf("the part reached another tenant's row: %s", part)
	}

	if err := db.Run(tenancy.WithTenant(t.Context(), acmeTenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return internal.PersonalData().Data.Erase(ctx, tx, subjectRow)
	}); err != nil {
		t.Fatalf("erase: %v", err)
	}

	// The re-read the erasure itself runs, asked again from outside the transaction that
	// did the work: nothing in this tenant's tasks carries any of the three strings.
	for _, term := range []string{email, handle, name} {
		var n int
		err := db.Run(tenancy.WithTenant(t.Context(), acmeTenant), conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
			return tx.DB().Raw(`SELECT count(*) FROM tasks
				 WHERE title ILIKE '%'||?||'%' OR description ILIKE '%'||?||'%' OR resolution ILIKE '%'||?||'%'`,
				term, term, term).Scan(&n).Error
		})
		if err != nil {
			t.Fatalf("look for %s: %v", term, err)
		}
		if n != 0 {
			t.Errorf("%d of Acme's tasks still carry %q after the erasure", n, term)
		}
	}

	// The work records survive their own scrubbing, with their assignees intact: the
	// erasure removed what the rows said, not the rows — and the second one still reads
	// as a resolution, with a token where the address was.
	var kept, stillWritten int
	if err := db.Run(tenancy.WithTenant(t.Context(), acmeTenant), conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		if err := tx.DB().Raw(`SELECT count(*) FROM tasks WHERE assignee_id = ?`, subject).Scan(&kept).Error; err != nil {
			return err
		}
		return tx.DB().Raw(`SELECT count(*) FROM tasks WHERE resolution LIKE ?`, "%<erased%").Scan(&stillWritten).Error
	}); err != nil || kept != 1 || stillWritten != 1 {
		t.Errorf("the rows are not what an anonymisation leaves (one assigned to the person, one resolution reading as a scrubbed sentence): %d, %d, %v",
			kept, stillWritten, err)
	}

	// And Beta's row is exactly as it was. The erasure never named a tenant: the policy
	// is what bounded it, which is the only isolation worth testing because it is the
	// isolation that cannot be forgotten by a future edit to this file.
	var untouched int
	err = db.Run(tenancy.WithTenant(t.Context(), other), conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw(`SELECT count(*) FROM tasks WHERE id = ? AND description LIKE ?`, betaTask, "%"+name+"%").Scan(&untouched).Error
	})
	if err != nil || untouched != 1 {
		t.Errorf("erasing Acme's person changed Beta's row: %d, %v", untouched, err)
	}

	// A person this tenant does not hold exports as nothing, not as another tenant's
	// rows: the same subject id, asked of Beta, answers with an empty part.
	var empty json.RawMessage
	if err := db.Run(tenancy.WithTenant(t.Context(), other), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		empty, err = internal.PersonalData().Data.Export(ctx, tx, subjectRow)
		return err
	}); err != nil || strings.Count(string(empty), `"id"`) != 0 {
		t.Errorf("a wrong-tenant export answered with rows: %s, %v", empty, err)
	}
}
