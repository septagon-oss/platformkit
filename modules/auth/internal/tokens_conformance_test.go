package internal_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	"github.com/septagon-oss/platformkit/modules/auth/internal"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/user"
)

// TestTheServiceConformsToTheTokenRules runs the key suite the fake runs against
// the real service, a real Postgres and a tenant transaction, presenting each
// key through Authenticate as a request's Authorization header.
func TestTheServiceConformsToTheTokenRules(t *testing.T) {
	authtest.RunTokens(t, func(t *testing.T, run func(authtest.TokenFixture)) {
		_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
		users := realUsers()
		svc := internal.NewService(users, nil, internal.Delivery{})
		seed(t, conn, acme)
		ctx := httpx.WithConn(tenancy.WithTenant(t.Context(), acme), conn)
		err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			run(authtest.TokenFixture{
				Fixture: authtest.Fixture{
					Ctx: ctx, Tx: tx, Service: svc,
					Published: func() []string { return outbox(t, tx) },
					Role: func(name string, permissions ...string) {
						err := tx.DB().Exec("INSERT INTO roles (tenant_id, name, permissions) VALUES (?, ?, ?)",
							acme.ID, name, pq.StringArray(permissions)).Error
						if err != nil {
							t.Fatalf("grant %s: %v", name, err)
						}
					},
					User: func(email, password string, roles ...string) uuid.UUID {
						u, err := users.Invite(ctx, tx, email, "")
						if err != nil {
							t.Fatalf("invite %s: %v", email, err)
						}
						if err := users.SetPassword(ctx, tx, u.ID, password); err != nil {
							t.Fatalf("set a password for %s: %v", email, err)
						}
						if _, err := users.SetRoles(ctx, tx, u.ID, roles); err != nil {
							t.Fatalf("grant %v to %s: %v", roles, email, err)
						}
						return u.ID
					},
				},
				Tokens: svc,
				Caller: func(token string) (tenancy.Principal, bool) {
					r := httptest.NewRequest(http.MethodPost, "http://"+host+"/api/v1/auth/tokens/revoke-self", nil)
					r.Header.Set("Authorization", "Bearer "+token)
					caller, ok, err := svc.Authenticate(ctx, tx, r)
					if err != nil {
						t.Fatalf("Authenticate: %v", err)
					}
					return caller, ok
				},
				// A key's expiry must follow its issue (a CHECK), and a microsecond
				// after issue is already past by the time the case reads it.
				Expire: func(id uuid.UUID) {
					err := tx.DB().Exec("UPDATE api_tokens SET expires_at = created_at + interval '1 microsecond' WHERE id = ?", id).Error
					if err != nil {
						t.Fatalf("expire %s: %v", id, err)
					}
				},
			})
			return errRollback
		})
		if !errors.Is(err, errRollback) {
			t.Fatalf("the case's transaction: %v", err)
		}
	})
}
