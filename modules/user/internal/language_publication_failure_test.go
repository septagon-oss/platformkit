package internal_test

import (
	"context"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/user"
	"github.com/septagon-oss/platformkit/modules/user/contracts"
)

func TestFailedLanguagePublicationReturnsNoChangedUser(t *testing.T) {
	owner, conn := dbtest.Schema(t, user.Migrations)
	person := invite(t, conn, "ada@acme.example.com")
	if _, err := owner.ExecContext(t.Context(), `ALTER TABLE platformkit_outbox
		ADD CONSTRAINT language_publication_unavailable CHECK (name <> 'user.language_set')`); err != nil {
		t.Fatal(err)
	}
	svc := newService()
	ctx := asSomeone(tenancy.WithTenant(t.Context(), acme), person)
	err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		row, err := svc.SetPreferredLocaleAt(ctx, tx, 0, "pt-PT")
		if err == nil {
			t.Fatal("the command did not report the outbox failure")
		}
		if row != nil {
			t.Errorf("failed publication returned language=%q revision=%d; an aborted command must return no user row", row.PreferredLocale, row.LocaleRevision)
		}
		return err
	})
	if err == nil {
		t.Fatal("the failed publication unexpectedly committed")
	}
	if got := preferred(t, conn, person); got != "" {
		t.Errorf("publication failure committed preference %q", got)
	}
	if n := countNamed(outboxNames(t, conn), contracts.EventLanguageSet); n != 0 {
		t.Errorf("publication failure left %d language events", n)
	}
}
