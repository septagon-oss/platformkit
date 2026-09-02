package internal_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	"github.com/septagon-oss/platformkit/modules/auth/internal"
	"github.com/septagon-oss/platformkit/modules/user"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// realUsers is the user module's own service, reached the way any other module
// reaches it: through modules/user, because modules/user/internal is closed to
// everything outside that module. The compiler is the boundary (idea 3), and
// this line is a test being held to it like anything else.
func realUsers() usercontracts.Service {
	svc, _ := user.Module(user.Deps{})
	return svc
}

var (
	acme        = tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}
	globex      = tenancy.Tenant{ID: uuid.New(), Slug: "globex", Name: "Globex"}
	errRollback = errors.New("rolled back on purpose")
	nobody      = contracts.Client{UserAgent: "go-test", IP: "203.0.113.1"}
)

// TestServiceConforms runs the same suite the fake runs, against the real
// service, a real Postgres and a real tenant transaction.
//
// The case's transaction is rolled back, but the failed-login events are not:
// Login writes those in a transaction of its own, because the response that
// carries them is a 401 and kit/httpx does not commit a 401. That is exactly
// why the harness reads the outbox rather than a slice — the two kinds of write
// are indistinguishable there, which is what the suite is entitled to assume.
func TestServiceConforms(t *testing.T) {
	authtest.RunService(t, func(t *testing.T, run func(authtest.Fixture)) {
		_, conn := dbtest.Schema(t)
		users := realUsers()
		svc := internal.NewService(users)
		seed(t, conn, svc, acme)

		ctx := httpx.WithConn(tenancy.WithTenant(t.Context(), acme), conn)
		err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			run(authtest.Fixture{
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
					if password != "" {
						if err := users.SetPassword(ctx, tx, u.ID, password); err != nil {
							t.Fatalf("set a password for %s: %v", email, err)
						}
					}
					if len(roles) > 0 {
						if _, err := users.SetRoles(ctx, tx, u.ID, roles); err != nil {
							t.Fatalf("grant %v to %s: %v", roles, email, err)
						}
					}
					return u.ID
				},
			})
			return errRollback
		})
		if !errors.Is(err, errRollback) {
			t.Fatalf("the case's transaction: %v", err)
		}
	})
}

// seed installs the two roles a tenant is created with, in a cross-tenant
// transaction, exactly as the tenant module's create hook does.
func seed(t *testing.T, conn *db.Conn, svc contracts.Service, tenant tenancy.Tenant) {
	t.Helper()
	err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		return svc.SeedRoles(ctx, tx, tenant.ID)
	})
	if err != nil {
		t.Fatalf("seed the roles of %s: %v", tenant.Slug, err)
	}
}

// outbox is this module's events, in order: the ones written in the caller's
// transaction and the ones written beside it.
//
// Only this module's, because the harness gets its users through the user
// module's real service and that publishes as it goes — user.invited,
// user.password_set. Filtering here rather than in the suite keeps the suite's
// claim exact: it is about what signing in publishes, and the fake has no user
// module behind it to publish anything else.
func outbox(t *testing.T, tx db.Tx[db.Tenant]) []string {
	t.Helper()
	var names []string
	err := tx.DB().Table("platformkit_outbox").
		Where("name LIKE 'auth.%'").Order("created_at, id").Pluck("name", &names).Error
	if err != nil {
		t.Fatalf("read the outbox: %v", err)
	}
	return names
}

// TestASessionFromAnotherTenantIsNotASessionHere is the claim the whole design
// rests on, and it is a database claim rather than a Go one: the session row
// exists, its id is presented at the other tenant's host, and the query that
// looks for it returns nothing because the policy does. No code compares two
// tenant ids; there is no code to get wrong.
func TestASessionFromAnotherTenantIsNotASessionHere(t *testing.T) {
	_, conn := dbtest.Schema(t)
	users := realUsers()
	svc := internal.NewService(users)
	seed(t, conn, svc, acme)
	seed(t, conn, svc, globex)

	var session uuid.UUID
	ctx := httpx.WithConn(t.Context(), conn)
	err := db.Run(tenancy.WithTenant(ctx, acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		u, err := users.Invite(ctx, tx, "ada@acme.example.com", "Ada")
		if err != nil {
			return err
		}
		if err := users.SetPassword(ctx, tx, u.ID, authtest.Password); err != nil {
			return err
		}
		s, _, err := svc.Login(ctx, tx, "ada@acme.example.com", authtest.Password, nobody)
		if err != nil {
			return err
		}
		session = s.ID
		return nil
	})
	if err != nil {
		t.Fatalf("sign in at acme: %v", err)
	}

	// The same session id, at the other tenant's host.
	err = db.Run(tenancy.WithTenant(ctx, globex), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if _, err := svc.Identify(ctx, tx, session, nobody); !errors.Is(err, crud.ErrNotFound) {
			t.Errorf("acme's session at globex = %v, want ErrNotFound", err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("the globex transaction: %v", err)
	}
}

// TestAnExpiredSessionIsNobody, and TestTheExpirySlidesOnUse: the two halves of
// a thirty-day session that a laptop in a drawer loses and a person who works
// every day keeps.
func TestAnExpiredSessionIsNobodyAndUseSlidesTheExpiry(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	users := realUsers()
	svc := internal.NewService(users)
	seed(t, conn, svc, acme)
	ctx := httpx.WithConn(t.Context(), conn)

	var live, expired uuid.UUID
	err := db.Run(tenancy.WithTenant(ctx, acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		u, err := users.Invite(ctx, tx, "ada@acme.example.com", "Ada")
		if err != nil {
			return err
		}
		if err := users.SetPassword(ctx, tx, u.ID, authtest.Password); err != nil {
			return err
		}
		for _, into := range []*uuid.UUID{&live, &expired} {
			s, _, err := svc.Login(ctx, tx, "ada@acme.example.com", authtest.Password, nobody)
			if err != nil {
				return err
			}
			*into = s.ID
		}
		return nil
	})
	if err != nil {
		t.Fatalf("sign in twice: %v", err)
	}

	// Age one session past its expiry, and push the other's last use back far
	// enough that the next request touches it.
	exec(t, admin, `UPDATE sessions SET expires_at = now() - interval '1 minute' WHERE id = $1`, expired)
	exec(t, admin, `UPDATE sessions SET last_seen_at = now() - interval '1 hour' WHERE id = $1`, live)
	var before time.Time
	row(t, admin, `SELECT expires_at FROM sessions WHERE id = $1`, live).Scan(&before)

	err = db.Run(tenancy.WithTenant(ctx, acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if _, err := svc.Identify(ctx, tx, expired, nobody); !errors.Is(err, crud.ErrNotFound) {
			t.Errorf("an expired session = %v, want ErrNotFound", err)
		}
		if _, err := svc.Identify(ctx, tx, live, nobody); err != nil {
			t.Errorf("a live session: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("the identifying transaction: %v", err)
	}

	var after time.Time
	row(t, admin, `SELECT expires_at FROM sessions WHERE id = $1`, live).Scan(&after)
	if !after.After(before) {
		t.Errorf("the expiry did not slide: %s then %s", before, after)
	}

	// And a second use inside the throttle writes nothing, which is what keeps
	// a read-only page load from taking a row lock on the session it read.
	var again time.Time
	err = db.Run(tenancy.WithTenant(ctx, acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := svc.Identify(ctx, tx, live, nobody)
		return err
	})
	if err != nil {
		t.Fatalf("the second use: %v", err)
	}
	row(t, admin, `SELECT expires_at FROM sessions WHERE id = $1`, live).Scan(&again)
	if !again.Equal(after) {
		t.Errorf("the expiry moved twice inside %s: %s then %s", contracts.SessionTouch, after, again)
	}
}

// TestDeactivatingSomebodyEndsTheirSessions without walking a list of them:
// a session is only honoured for an active user, so one column does it.
func TestDeactivatingSomebodyEndsTheirSessions(t *testing.T) {
	_, conn := dbtest.Schema(t)
	users := realUsers()
	svc := internal.NewService(users)
	seed(t, conn, svc, acme)
	ctx := httpx.WithConn(t.Context(), conn)

	err := db.Run(tenancy.WithTenant(ctx, acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		u, err := users.Invite(ctx, tx, "ada@acme.example.com", "Ada")
		if err != nil {
			return err
		}
		if err := users.SetPassword(ctx, tx, u.ID, authtest.Password); err != nil {
			return err
		}
		session, _, err := svc.Login(ctx, tx, "ada@acme.example.com", authtest.Password, nobody)
		if err != nil {
			return err
		}
		if _, err := svc.Identify(ctx, tx, session.ID, nobody); err != nil {
			t.Fatalf("Identify before deactivation: %v", err)
		}
		if _, err := users.Deactivate(ctx, tx, u.ID); err != nil {
			return err
		}
		if _, err := svc.Identify(ctx, tx, session.ID, nobody); !errors.Is(err, crud.ErrNotFound) {
			t.Errorf("a deactivated user's session = %v, want ErrNotFound", err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("the transaction: %v", err)
	}
}

// TestAFailedLoginIsRecordedThoughItsRequestIsRolledBack. A 401 is a response
// of 400 or worse, so the request's transaction never commits — and "this
// account was attacked and nothing recorded it" is the one case where that
// matters, which is why the event is written beside it.
func TestAFailedLoginIsRecordedThoughItsRequestIsRolledBack(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	users := realUsers()
	svc := internal.NewService(users)
	seed(t, conn, svc, acme)
	ctx := httpx.WithConn(t.Context(), conn)

	_ = db.Run(tenancy.WithTenant(ctx, acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if _, _, err := svc.Login(ctx, tx, "nobody@acme.example.com", authtest.Password, nobody); !errors.Is(err, contracts.ErrCredentials) {
			t.Errorf("Login = %v, want ErrCredentials", err)
		}
		return errRollback // what kit/httpx does to a 401
	})

	var count int
	var email string
	err := admin.QueryRowContext(t.Context(),
		`SELECT count(*), coalesce(max(payload->>'email'), '') FROM platformkit_outbox WHERE name = $1`,
		contracts.EventLoginFailed).Scan(&count, &email)
	if err != nil {
		t.Fatalf("read the outbox: %v", err)
	}
	if count != 1 || email != "nobody@acme.example.com" {
		t.Errorf("the outbox holds %d failed logins for %q, want one for the address that was tried", count, email)
	}
}

func exec(t *testing.T, admin *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := admin.ExecContext(t.Context(), query, args...); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
}

func row(t *testing.T, admin *sql.DB, query string, args ...any) *sql.Row {
	t.Helper()
	return admin.QueryRowContext(t.Context(), query, args...)
}
