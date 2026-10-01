package seed

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// TestDemoRecordsFollowTheTenantRowNotTheCallersValue pins docs/seed.md's
// "Demo requested for a tenant whose persisted Demo is false: refuse". The
// tenants row says false; the caller hands db.Run a tenancy.Tenant value that
// says true, which any composition or command can construct. The persisted
// flag decides: the run is refused, the owner is asked for nothing and no
// provenance row exists afterwards.
func TestDemoRecordsFollowTheTenantRowNotTheCallersValue(t *testing.T) {
	admin, app := dbtest.Schema(t)
	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "plain", Demo: true}
	if _, err := admin.ExecContext(t.Context(), `INSERT INTO tenants (id, slug, name, demo) VALUES ($1, $2, $2, false)`,
		tenant.ID, tenant.Slug); err != nil {
		t.Fatal(err)
	}
	writer := newFakeWriter("contents", "content", "content", true)
	service, err := New(Deps{Files: fstest.MapFS{
		"seed/starter/contents.yaml": {Data: []byte("apiVersion: platformkit.seed/v1\nresource: contents\nrecords: []")},
		"seed/demo/contents.yaml":    {Data: []byte("apiVersion: platformkit.seed/v1\nresource: contents\nrecords:\n  - key: tour\n    fields: {title: Tour}")},
	}, Root: "seed", Clock: seedAt, Writers: []Writer{writer}, Authorize: &fakeGrant{}})
	if err != nil {
		t.Fatal(err)
	}
	var applyErr error
	if err := db.Run(tenancy.WithTenant(t.Context(), tenant), app, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, applyErr = service.Apply(ctx, tx, Selection{Demo: true})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if applyErr == nil || !strings.Contains(applyErr.Error(), "non-demo tenant") {
		t.Errorf("Apply error = %v; want the non-demo refusal for a tenant whose row says demo=false", applyErr)
	}
	if len(writer.writes) != 0 {
		t.Errorf("demo records reached the owner of a non-demo tenant: %v", writer.writes)
	}
	var keys int
	if err := admin.QueryRowContext(t.Context(), `SELECT count(*) FROM seed_keys WHERE tenant_id = $1 AND kind = 'demo'`,
		tenant.ID).Scan(&keys); err != nil {
		t.Fatal(err)
	}
	if keys != 0 {
		t.Errorf("a non-demo tenant holds %d demo provenance rows", keys)
	}
}
