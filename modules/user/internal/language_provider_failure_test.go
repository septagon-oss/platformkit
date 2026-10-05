package internal_test

import (
	"context"
	"errors"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/user/contracts"
	"github.com/septagon-oss/platformkit/modules/user/internal"
)

func TestUnavailableLanguagePolicyWritesNothing(t *testing.T) {
	conn, ctx := tenantWith(t)
	person := invite(t, conn, "language-policy@acme.example.com")
	unavailable := errors.New("language policy unavailable")
	policy := &contracts.LocalePolicyFunc{Ask: func(context.Context, db.Tx[db.Tenant]) (tenancy.Languages, error) {
		return tenancy.Languages{}, unavailable
	}}
	svc := internal.NewService(&contracts.AdministrationFunc{Ask: administering}, allowGranting, policy)
	before := len(outboxNames(t, conn))
	err := db.Run(asSomeone(ctx, person), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		row, err := svc.SetPreferredLocaleAt(ctx, tx, 0, "pt-PT")
		if !errors.Is(err, unavailable) {
			t.Errorf("policy failure = %v, want unavailable", err)
		}
		if row != nil {
			t.Error("policy failure returned a user row")
		}
		// Commit to expose any side effect the refusal might have left behind.
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := preferred(t, conn, person); got != "" {
		t.Errorf("policy failure wrote language %q", got)
	}
	if got := len(outboxNames(t, conn)); got != before {
		t.Errorf("policy failure changed event count from %d to %d", before, got)
	}
}
