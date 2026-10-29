package limit_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/limit"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func TestLimiterConstructorsShareBucketsAndDeferConnections(t *testing.T) {
	for _, name := range []appname.Name{"", "pkit", "billing-service", "BAD"} {
		t.Run(name.String(), func(t *testing.T) {
			admin, conn := dbtest.Schema(t)
			calls := 0
			conns := func(context.Context) (*db.Conn, bool) { calls++; return conn, true }
			current := limit.PostgresFor(name, conns)
			old := limit.PostgresOf(name, conns)
			if name == "" {
				old = limit.Postgres(conns)
			}
			if calls != 0 {
				t.Fatal("construction asked for a connection")
			}
			tenant := tenancy.Tenant{ID: uuid.MustParse("33333333-3333-4333-8333-333333333333")}
			ctx := tenancy.WithTenant(t.Context(), tenant)
			for i, l := range []limit.Limiter{old, current, limit.PostgresOf(name, conns)} {
				ok, left, retry, err := l.Reserve(ctx, "login", 2, window)
				if err != nil || ok != (i < 2) || left != max(1-i, 0) || retry <= 0 || retry > window {
					t.Fatalf("attempt %d: %v, %d, %s, %v", i, ok, left, retry, err)
				}
			}
			prefix := ""
			if name != "" {
				prefix = name.String() + "/"
				if name == "BAD" {
					prefix = "/"
				}
			}
			if got := storedKeys(t, admin, 1)[0]; got != prefix+"33333333-3333-4333-8333-333333333333/login" {
				t.Fatalf("stored key = %q", got)
			}
			for _, l := range []limit.Limiter{old, current} {
				if n, _, err := l.Count(ctx, "login", window); err != nil || n != 3 {
					t.Fatalf("shared count = %d, %v", n, err)
				}
			}
			other := limit.PostgresFor("another", conns)
			if n, _, err := other.Count(ctx, "login", window); err != nil || n != 0 {
				t.Fatalf("another application = %d, %v", n, err)
			}
			otherTenant := tenancy.WithTenant(t.Context(), tenancy.Tenant{ID: uuid.New()})
			if n, _, err := current.Count(otherTenant, "login", window); err != nil || n != 0 {
				t.Fatalf("another tenant = %d, %v", n, err)
			}
			for _, l := range []limit.Limiter{current, old} {
				if err := l.Forget(ctx, "login"); err != nil {
					t.Fatal(err)
				}
				if n, _, err := current.Count(ctx, "login", window); err != nil || n != 0 {
					t.Fatalf("forgotten count = %d, %v", n, err)
				}
			}
			if _, _, err := current.Allow(t.Context(), "login", 2, window); err != nil {
				t.Fatal(err)
			}
			if got := storedKeys(t, admin, 1)[0]; got != prefix+"00000000-0000-0000-0000-000000000000/login" {
				t.Fatalf("installation key = %q", got)
			}
		})
	}
}
