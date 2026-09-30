package internal

import (
	"context"
	"fmt"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// path is the collection. The control plane is served on every tenant's host,
// because there is nowhere else to serve it from: an installation has no host
// of its own, only its customers'.
//
// So the permission alone does not keep it safe, and the earlier version of
// this comment claiming it did was wrong. tenant:manage is an ordinary string
// in an ordinary roles table, and every tenant's admin role holds the wildcard
// by construction — which used to mean any customer's administrator could list,
// create and suspend the tenants beside them, at their own host, with the
// credentials they were legitimately given. What keeps it safe is that these
// five routes declare httpx.OperatorPermission: the kernel refuses the request
// at any tenant but the operator's own before it asks the roles table anything,
// and no wildcard satisfies the grant even there.
const path = "/tenants"

// RegisterRoutes mounts the five control-plane routes.
//
// They are written by hand rather than mounted from a rest.Spec because a
// tenant is not a crud.Entity: it carries no tenant_id, so the generic
// repository — which stamps one from the transaction — has nothing to stamp.
// That is the whole cost of the exception, and it is five short handlers.
//
// Every one of them opens a transaction of its own. The request already holds a
// tenant transaction, because recognising the caller was a query in it, and a
// system transaction cannot widen a tenant one: db.Detached is what says so out
// loud. The consequence is written down where it matters — a tenant created
// here is created whether or not the response afterwards reaches the caller.
func RegisterRoutes(r *httpx.Router, svc contracts.Service, invite contracts.Inviter, token tenancy.SystemToken) {
	system := func(ctx context.Context, fn func(context.Context, db.Tx[db.System]) error) error {
		conn, ok := httpx.ConnFrom(ctx)
		if !ok {
			return problem.New(http.StatusServiceUnavailable, "the database is not reachable right now")
		}
		return db.RunSystem(db.Detached(ctx), conn, token, fn)
	}

	httpx.Register(r, op("list", http.MethodGet, path, 0, "List the tenants",
		"Every tenant of this installation, suspended ones included.", nil),
		httpx.OperatorPermission(contracts.PermissionTenantManage),
		func(ctx context.Context, _ *struct{}) (*listOutput, error) {
			out := &listOutput{}
			err := system(ctx, func(ctx context.Context, tx db.Tx[db.System]) error {
				items, err := svc.List(ctx, tx)
				out.Body.Items, out.Body.Total = items, len(items)
				return err
			})
			return out, rest.Fault(err)
		})

	httpx.Register(r, op("create", http.MethodPost, path, http.StatusCreated, "Create a tenant",
		"Writes the tenant, its first host and the roles a tenant starts with, in one transaction.",
		[]string{contracts.EventCreated}),
		httpx.OperatorPermission(contracts.PermissionTenantManage),
		func(ctx context.Context, in *createInput) (*itemOutput, error) {
			out := &itemOutput{}
			err := system(ctx, func(ctx context.Context, tx db.Tx[db.System]) error {
				t, err := svc.Create(ctx, tx, in.Body)
				out.Body = t
				return err
			})
			return out, rest.Fault(err)
		})

	httpx.Register(r, op("read", http.MethodGet, path+"/{id}", 0, "Read a tenant", "", nil),
		httpx.OperatorPermission(contracts.PermissionTenantManage),
		func(ctx context.Context, in *idInput) (*itemOutput, error) {
			out := &itemOutput{}
			err := system(ctx, func(ctx context.Context, tx db.Tx[db.System]) error {
				t, err := svc.Get(ctx, tx, in.ID)
				out.Body = t
				return err
			})
			return out, rest.Fault(err)
		})

	httpx.Register(r, op("suspend", http.MethodPost, path+"/{id}/suspend", 0, "Suspend a tenant",
		"Stops the tenant being served: its hosts answer as though no site were there. Suspending it again changes nothing.",
		[]string{contracts.EventSuspended}),
		httpx.OperatorPermission(contracts.PermissionTenantManage),
		func(ctx context.Context, in *idInput) (*itemOutput, error) {
			out := &itemOutput{}
			err := system(ctx, func(ctx context.Context, tx db.Tx[db.System]) error {
				t, err := svc.Suspend(ctx, tx, in.ID)
				out.Body = t
				return err
			})
			if err == nil {
				// The resolution cache believes a host for half a minute, and
				// a suspension that takes effect in half a minute is a
				// suspension somebody has to explain.
				for _, host := range out.Body.Hosts {
					r.InvalidateHost(host)
				}
			}
			return out, rest.Fault(err)
		})

	httpx.Register(r, op("set-locale", http.MethodPost, path+"/{id}/locale", 0, "Say which languages a tenant is served in",
		"Sets the language a request with no usable preference is answered in, and the set the browser's list is intersected with. Setting the same pair again changes nothing and publishes nothing. The languages a tenant may be answered in are a declaration about a customer, which is why this is the operator's route and not the tenant's: a tenant's own copy is a different capability, in a table a tenant can write.",
		[]string{contracts.EventLocaleSet}),
		httpx.OperatorPermission(contracts.PermissionTenantManage),
		func(ctx context.Context, in *localeInput) (*itemOutput, error) {
			out := &itemOutput{}
			err := system(ctx, func(ctx context.Context, tx db.Tx[db.System]) error {
				t, err := svc.SetLocale(ctx, tx, in.ID, in.Body)
				out.Body = t
				return err
			})
			if err == nil {
				// The cached resolution carries the tenant's languages with it, so
				// the languages of a page are half a minute stale unless the
				// resolution is forgotten here — the same reason a suspension does
				// it, and the same one query this route already made.
				for _, host := range out.Body.Hosts {
					r.InvalidateHost(host)
				}
			}
			return out, rest.Fault(err)
		})

	httpx.Register(r, op("add-host", http.MethodPost, path+"/{id}/hosts", http.StatusCreated, "Give a tenant another host",
		"Adding a host the tenant already answers at changes nothing, unless it makes it the primary one. The primary host is what every absolute URL for this tenant is built on, so a link in a mail is a link to the name its people know.",
		[]string{contracts.EventHostAdded}),
		httpx.OperatorPermission(contracts.PermissionTenantManage),
		func(ctx context.Context, in *hostInput) (*itemOutput, error) {
			out := &itemOutput{}
			err := system(ctx, func(ctx context.Context, tx db.Tx[db.System]) error {
				t, err := svc.AddHost(ctx, tx, in.ID, in.Body.Host, in.Body.Primary)
				out.Body = t
				return err
			})
			return out, rest.Fault(err)
		})

	// The two provider routes: which identity provider this tenant's people sign
	// in against, and taking it away again. They are the control plane's because
	// the row they write is the control plane's — a tenant transaction reads its
	// own provider and may not choose it (migrations/000030) — and because which
	// directory a company's people live in is a decision somebody makes about a
	// customer, in the same shape as which languages they are served in.
	httpx.Register(r, op("set-oidc", http.MethodPost, path+"/{id}/oidc", 0, "Say which identity provider a tenant signs in against",
		"Sets the issuer, the client and the secret reference for one tenant, and what an address the provider vouches for and this tenant has no account for does: disabled, existing or provision. Setting the same values again changes nothing and publishes nothing. The secret itself is never written here: the reference names where it is kept, because a row is copied into the audit trail and a secret in a payload is a secret in the trail.",
		[]string{contracts.EventOIDCSet}),
		httpx.OperatorPermission(contracts.PermissionTenantManage),
		func(ctx context.Context, in *oidcInput) (*itemOutput, error) {
			out := &itemOutput{}
			err := system(ctx, func(ctx context.Context, tx db.Tx[db.System]) error {
				t, err := svc.SetOIDC(ctx, tx, in.ID, in.Body)
				out.Body = t
				return err
			})
			return out, rest.Fault(err)
		})

	httpx.Register(r, op("clear-oidc", http.MethodPost, path+"/{id}/oidc/clear", 0, "Take a tenant's identity provider away",
		"After this the tenant's single sign-on answers 404 and its people sign in with a password. People already signed in stay signed in: taking a company's single sign-on away is not a revocation of anybody's session, and revocations are somebody else's decision.",
		[]string{contracts.EventOIDCCleared}),
		httpx.OperatorPermission(contracts.PermissionTenantManage),
		func(ctx context.Context, in *idInput) (*itemOutput, error) {
			out := &itemOutput{}
			err := system(ctx, func(ctx context.Context, tx db.Tx[db.System]) error {
				t, err := svc.ClearOIDC(ctx, tx, in.ID)
				out.Body = t
				return err
			})
			return out, rest.Fault(err)
		})

	if invite == nil {
		return
	}
	// The sixth route, and the one that makes the other five worth having.
	//
	// The control plane could create a tenant and could not put anybody in it.
	// Every route that makes a user is a tenant route, authorized inside that
	// tenant's own transaction, and the operator is at their own host — so a
	// tenant created here had no first administrator and no way to get one
	// except SQL, which is the answer that means the feature is missing.
	//
	// It is not "create a user in any tenant". The roles are this module's
	// decision and not the caller's, no password crosses the boundary, and what
	// is created is somebody invited: they cannot sign in until they have read
	// the mail that user.invited causes. So the worst an operator can do with
	// it is offer somebody a way into a tenant, which is what the control plane
	// is for.
	httpx.Register(r, op("invite", http.MethodPost, path+"/{id}/invite", http.StatusCreated,
		"Give a tenant its first administrator",
		"Creates an invited administrator in the named tenant and publishes user.invited, which is what mails them a link to choose a password. No password crosses the control plane, and the person cannot sign in until they have followed the link. Inviting an address the tenant already has is a conflict.",
		[]string{usercontracts.EventInvited}),
		httpx.OperatorPermission(contracts.PermissionTenantManage),
		func(ctx context.Context, in *inviteInput) (*itemOutput, error) {
			out := &itemOutput{}
			err := system(ctx, func(ctx context.Context, tx db.Tx[db.System]) error {
				t, err := svc.Get(ctx, tx, in.ID)
				if err != nil {
					return err
				}
				out.Body = t
				return invite.Invite(ctx, tx, t.ID, in.Body.Email, in.Body.DisplayName)
			})
			return out, rest.Fault(err)
		})
}

// op builds one operation, including the events its handler will publish, which
// is the declaration kit/app's boot gate reads back.
func op(verb, method, at string, status int, summary, description string, published []string) huma.Operation {
	o := huma.Operation{
		OperationID:   "tenant-tenant-" + verb,
		Method:        method,
		Path:          at,
		Summary:       summary,
		Description:   description,
		Tags:          []string{"tenant"},
		DefaultStatus: status,
		Errors: []int{http.StatusNotFound, http.StatusConflict,
			http.StatusUnprocessableEntity, http.StatusServiceUnavailable},
	}
	if len(published) > 0 {
		o.Extensions = map[string]any{httpx.EventsExtension: published}
	}
	return o
}

type oidcInput struct {
	ID   uuid.UUID              `path:"id" format:"uuid" doc:"The tenant's id"`
	Body contracts.OIDCSettings `required:"true"`
}

type idInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"The tenant's id"`
}

type createInput struct {
	Body contracts.NewTenant `required:"true"`
}

type hostInput struct {
	ID   uuid.UUID `path:"id" format:"uuid" doc:"The tenant's id"`
	Body struct {
		Host string `json:"host" minLength:"1" maxLength:"253" doc:"Another host this tenant answers at"`
		// A tenant has exactly one primary host and it is the first one it was
		// created with until somebody says otherwise, so this defaults to
		// false: adding a name is not moving everybody's links to it.
		Primary bool `json:"primary,omitempty" doc:"Make this the host absolute URLs for this tenant are built on"`
	}
}

type localeInput struct {
	ID   uuid.UUID           `path:"id" format:"uuid" doc:"The tenant's id"`
	Body contracts.SetLocale `required:"true"`
}

type inviteInput struct {
	ID   uuid.UUID `path:"id" format:"uuid" doc:"The tenant to invite somebody into"`
	Body struct {
		Email       string `json:"email" format:"email" maxLength:"320" doc:"The address to invite" example:"ada@acme.example.com"`
		DisplayName string `json:"displayName,omitempty" maxLength:"200" doc:"Name to show" example:"Ada Lovelace"`
	} `required:"true"`
}

type itemOutput struct {
	Body *contracts.Tenant
}

type listOutput struct {
	Body struct {
		Items []*contracts.Tenant `json:"items"`
		Total int                 `json:"total"`
	}
}

// Bootstrap is the first tenant of an installation, created from the command
// line rather than from a request.
//
// It refuses when any tenant already exists, and that refusal is the whole
// point: this is the one write that runs with no caller to authorize, so the
// condition that makes it safe is that it can only ever happen once. Two of
// these racing is one of them: kit/app.Bootstrap takes an advisory lock for the
// transaction before this reads the list.
//
// The tenant it creates is the operator's — the installation's own, whose
// administrators may reach the control plane at all. This line is the only
// writer of that flag in the application: NewTenant.Operator is json:"-", so no
// request body carries one.
func Bootstrap(ctx context.Context, tx db.Tx[db.System], svc contracts.Service, in contracts.NewTenant) (*contracts.Tenant, error) {
	existing, err := svc.List(ctx, tx)
	if err != nil {
		return nil, err
	}
	if len(existing) > 0 {
		return nil, fmt.Errorf("%w: this installation already has %d tenant(s); create the next one through %s",
			crud.ErrConflict, len(existing), path)
	}
	in.Operator = true
	return svc.Create(ctx, tx, in)
}

// compile-time proof that the service is the two things the kernel wires.
var (
	_ httpx.TenantLoader = (contracts.Service)(nil)
	_ interface {
		List(context.Context, db.Tx[db.System]) ([]tenancy.Tenant, error)
	} = contracts.Active{}
)
