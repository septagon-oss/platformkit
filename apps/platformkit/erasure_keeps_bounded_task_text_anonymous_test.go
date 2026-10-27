package main

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	subjectcontracts "github.com/septagon-oss/platformkit/modules/subject/contracts"
	taskcontracts "github.com/septagon-oss/platformkit/modules/task/contracts"
	"github.com/septagon-oss/platformkit/pkit"
)

func TestCompletedErasureLeavesNoNameInABoundedTaskTitle(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	svc, ok := pkit.Value[subjectcontracts.Service](composeReference(cfg, pkit.Development).plan())
	if !ok || svc == nil {
		t.Fatal("the composition provides no personal-data service")
	}
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	var tenant tenancy.Tenant
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		var err error
		tenant, err = c.testTenants().ByHost(ctx, tx, acmeHost)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	const name, email = "Jo Jones", "jo.jones@example.com"
	person := uuid.New()
	limit := 0
	for _, field := range crud.Fields[*taskcontracts.Task]() {
		if field.Name == "title" {
			limit = field.MaxLength
		}
	}
	prefix, suffix := name+": ", " / Jo Joneson"
	if limit <= len(prefix)+len(suffix) {
		t.Fatalf("title metadata has no usable capacity: %d", limit)
	}
	row := &taskcontracts.Task{Title: prefix + strings.Repeat("x", limit-len(prefix)-len(suffix)) + suffix}
	ctx := tenancy.WithTenant(t.Context(), tenant)
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := tx.DB().Exec(`INSERT INTO users (id, tenant_id, email, display_name) VALUES (?, ?, ?, ?)`,
			person, tenant.ID, email, name).Error; err != nil {
			return err
		}
		return crud.Create(ctx, tx, row)
	}); err != nil {
		t.Fatalf("seed the person and a valid task: %v", err)
	}
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := svc.Erase(ctx, tx, subjectcontracts.EraseInput{Subject: person, Confirm: email})
		return err
	}); err != nil {
		t.Fatalf("erase through the composed owners: %v", err)
	}
	if err := db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		kept, err := crud.Get[*taskcontracts.Task](tx, row.ID)
		if err != nil {
			return err
		}
		var completed int
		if err := tx.DB().Raw(`SELECT count(*) FROM platformkit_outbox WHERE name = ? AND payload->>'subject' = ?`,
			subjectcontracts.EventErased, person.String()).Scan(&completed).Error; err != nil {
			return err
		}
		if completed != 1 {
			t.Errorf("erasure completion events = %d, want 1", completed)
		}
		if regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\b`).MatchString(kept.Title) {
			t.Errorf("erasure committed %d completion event and left the person's whole name in the title: %q", completed, kept.Title)
		}
		return nil
	}); err != nil {
		t.Fatalf("read the committed task and erasure event: %v", err)
	}
}
