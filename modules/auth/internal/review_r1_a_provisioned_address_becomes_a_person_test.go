package internal_test

import (
	"context"
	"log/slog"
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/user"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// maker is what the contract docs say the product would wire: the user module,
// reached through the two methods a tenant transaction may use. It is written
// here because nothing else in the repository has ever written it —
// `grep -rn "Provision" --include=*_test.go` finds no implementation of
// contracts.Provisioner anywhere, in a test or in a composition.
type maker struct{ users usercontracts.Service }

func (m maker) Provision(ctx context.Context, tx db.Tx[db.Tenant], email, displayName string, roles []string) (uuid.UUID, error) {
	person, err := m.users.Invite(ctx, tx, email, displayName)
	if err != nil {
		return uuid.Nil, err
	}
	if _, err := m.users.SetRoles(ctx, tx, person.ID, roles); err != nil {
		return uuid.Nil, err
	}
	return person.ID, nil
}

// mountProvision is mountTwo with the third dependency the callback can use: a
// Provisioner. The arrangement is the one apps/platformkit refuses to wire — the
// composition's own comment says the product wires it "in one line" — so this is
// the only place the `provision` branch is reachable at all.
func mountProvision(t *testing.T, providers contracts.OIDCProviders, secrets contracts.Secrets, makes contracts.Provisioner) (chi.Router, *db.Conn) {
	t.Helper()
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	users, userModule := user.Module(user.Deps{Administration: &usercontracts.AdministrationFunc{Ask: auth.AdministeringRoles}})
	svc, authModule := auth.Module(auth.Deps{
		Users: users, Notify: &authtest.Notices{}, Mailer: &authtest.Mailbox{},
		Hosts: authtest.Host(host), OIDCProviders: providers, Secrets: secrets,
		Provisioner: makes, PublicHost: host,
	})
	seed(t, conn, acme)
	seed(t, conn, globex)
	api, router := httpx.New(httpx.Options{
		PublicHost: host, Tenants: twoSites{}, Conn: conn,
		Authorize: svc, Authenticate: svc.Authenticate, Log: slog.New(slog.DiscardHandler),
	})
	api.Declare([]tenancy.Grant{{Permission: contracts.PermissionRoleManage}})
	authModule.Routes(surfacesOf(api))
	userModule.Routes(api.Surfaces("user"))
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the mounted routes do not declare themselves: %v", err)
	}
	return router, conn
}

// TestAProvisionedAddressBecomesAPersonAndOnlyAPerson is the third registration
// mode, which no test in this repository has ever run.
//
// contracts/oidc_provider.go says `provision` is "the operator saying that an
// address the provider verified is on its own an account here", and that the
// person "is made with the named roles and nothing else"; internal/oidc.go says
// the same at the branch. The tenant module refuses to *store* provision without
// roles (migrations/000030's CHECK and the four cases in tenanttest), and the
// reference application refuses to *wire* a Provisioner at all — so the one
// arrangement where the branch runs is a composition nobody has ever assembled
// and no case has ever driven. A mode that only exists in an unexercised branch
// is a mode whose security property is a comment: here, that the person a
// verified address conjures does not arrive holding the administrator role.
//
// So the case drives the whole leg — start, a verified address with no account,
// callback — and then asks the tenant who is actually there: signed in, yes, and
// holding exactly the roles the tenant's row named. The assertion is read back
// from the user module in this tenant's own transaction, so it cannot be satisfied
// by anything the response says about itself.
func TestAProvisionedAddressBecomesAPersonAndOnlyAPerson(t *testing.T) {
	idp := authtest.NewIssuer(t)
	const address = "zoe@acme.example.com"
	router, conn := mountProvision(t, mapProviders{
		acme.ID: {Issuer: idp.URL, ClientID: "platformkit", SecretRef: "ACME_SECRET",
			RedirectPath: "/api/v1/auth/oidc/callback",
			Registration: contracts.RegistrationProvision, Roles: []string{contracts.RoleMember}},
	}, mapSecrets{"ACME_SECRET": "acme-secret"}, maker{users: realUsers()})

	challenge, state, cookie, code := start(t, router, host)
	if code != http.StatusSeeOther {
		t.Fatalf("a provision tenant's start = %d, want 303", code)
	}
	if challenge == "" {
		t.Fatal("the redirect carried no PKCE challenge")
	}
	idp.Issue("zoe-code", address, true, "platformkit", nonce(cookie))
	res := callback(t, router, host, "zoe-code", state, cookie)
	if res.Code != http.StatusSeeOther {
		t.Fatalf("a verified address at a provision tenant = %d %s, want 303 with a session",
			res.Code, res.Body.String())
	}
	if sessionCookie(res) == "" {
		t.Fatal("the provisioned person got no session cookie")
	}

	var roles []string
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn,
		func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			person, err := realUsers().ByEmail(ctx, tx, address)
			if err != nil {
				return err
			}
			roles = person.Roles
			return nil
		})
	if err != nil {
		t.Fatalf("read the person the callback made: %v", err)
	}
	if len(roles) != 1 || roles[0] != contracts.RoleMember {
		t.Errorf("the provisioned person holds %v, want exactly [%s]: the roles the row named and nothing else",
			roles, contracts.RoleMember)
	}
}

// TestTheProvisionPortIsAnswerableOverTheUserModule asks the question the
// composition's comment answers casually — "the product that wants it wires one
// over the user module here, in one line". `contracts.Provisioner.Provision` is
// handed a *tenant* transaction, so the implementation must be built from the
// two user-module methods a tenant transaction may use, `Invite` and `SetRoles`.
// If that is not possible, the port is not a port: the callback's `provision`
// branch would name a capability no composition could answer, and no amount of
// wiring at apps/platformkit would change that.
func TestTheProvisionPortIsAnswerableOverTheUserModule(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	users := realUsers()
	seed(t, conn, acme)

	ctx := httpx.WithConn(tenancy.WithTenant(t.Context(), acme), conn)
	var roles []string
	err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		id, err := maker{users: users}.Provision(ctx, tx, "zoe@acme.example.com", "zoe@acme.example.com",
			[]string{contracts.RoleMember})
		if err != nil {
			return err
		}
		person, err := users.Get(ctx, tx, id)
		if err != nil {
			return err
		}
		roles = person.Roles
		return nil
	})
	if err != nil {
		t.Fatalf("make the person the port promises: %v", err)
	}
	if len(roles) != 1 || roles[0] != contracts.RoleMember {
		t.Errorf("the person made through the port holds %v, want exactly [%s]", roles, contracts.RoleMember)
	}
}
