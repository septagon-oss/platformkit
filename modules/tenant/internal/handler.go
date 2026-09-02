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
)

// path is the collection. The control plane is served on every tenant's host,
// because there is nowhere else to serve it from: an installation has no host
// of its own, only its customers'. What keeps it safe is the permission —
// tenant:manage is granted in one tenant and reaches every tenant, which is a
// grant an operator makes deliberately and an ordinary administrator does not
// hold.
const path = "/api/v1/tenant/tenants"

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
func RegisterRoutes(api *httpx.API, svc contracts.Service, token tenancy.SystemToken) {
	system := func(ctx context.Context, fn func(context.Context, db.Tx[db.System]) error) error {
		conn, ok := httpx.ConnFrom(ctx)
		if !ok {
			return problem.New(http.StatusServiceUnavailable, "the database is not reachable right now")
		}
		return db.RunSystem(db.Detached(ctx), conn, token, fn)
	}

	httpx.Register(api, op("list", http.MethodGet, path, 0, "List the tenants",
		"Every tenant of this installation, suspended ones included.", nil),
		httpx.Permission(contracts.PermissionTenantManage),
		func(ctx context.Context, _ *struct{}) (*listOutput, error) {
			out := &listOutput{}
			err := system(ctx, func(ctx context.Context, tx db.Tx[db.System]) error {
				items, err := svc.List(ctx, tx)
				out.Body.Items, out.Body.Total = items, len(items)
				return err
			})
			return out, rest.Fault(err)
		})

	httpx.Register(api, op("create", http.MethodPost, path, http.StatusCreated, "Create a tenant",
		"Writes the tenant, its first host and the roles a tenant starts with, in one transaction.",
		[]string{contracts.EventCreated}),
		httpx.Permission(contracts.PermissionTenantManage),
		func(ctx context.Context, in *createInput) (*itemOutput, error) {
			out := &itemOutput{}
			err := system(ctx, func(ctx context.Context, tx db.Tx[db.System]) error {
				t, err := svc.Create(ctx, tx, in.Body)
				out.Body = t
				return err
			})
			return out, rest.Fault(err)
		})

	httpx.Register(api, op("read", http.MethodGet, path+"/{id}", 0, "Read a tenant", "", nil),
		httpx.Permission(contracts.PermissionTenantManage),
		func(ctx context.Context, in *idInput) (*itemOutput, error) {
			out := &itemOutput{}
			err := system(ctx, func(ctx context.Context, tx db.Tx[db.System]) error {
				t, err := svc.Get(ctx, tx, in.ID)
				out.Body = t
				return err
			})
			return out, rest.Fault(err)
		})

	httpx.Register(api, op("suspend", http.MethodPost, path+"/{id}/suspend", 0, "Suspend a tenant",
		"Stops the tenant being served: its hosts answer as though no site were there. Suspending it again changes nothing.",
		[]string{contracts.EventSuspended}),
		httpx.Permission(contracts.PermissionTenantManage),
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
					api.InvalidateHost(host)
				}
			}
			return out, rest.Fault(err)
		})

	httpx.Register(api, op("add-host", http.MethodPost, path+"/{id}/hosts", http.StatusCreated, "Give a tenant another host",
		"Adding a host the tenant already answers at changes nothing.",
		[]string{contracts.EventHostAdded}),
		httpx.Permission(contracts.PermissionTenantManage),
		func(ctx context.Context, in *hostInput) (*itemOutput, error) {
			out := &itemOutput{}
			err := system(ctx, func(ctx context.Context, tx db.Tx[db.System]) error {
				t, err := svc.AddHost(ctx, tx, in.ID, in.Body.Host)
				out.Body = t
				return err
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
	}
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
// condition that makes it safe is that it can only ever happen once.
func Bootstrap(ctx context.Context, tx db.Tx[db.System], svc contracts.Service, in contracts.NewTenant) (*contracts.Tenant, error) {
	existing, err := svc.List(ctx, tx)
	if err != nil {
		return nil, err
	}
	if len(existing) > 0 {
		return nil, fmt.Errorf("%w: this installation already has %d tenant(s); create the next one through %s",
			crud.ErrConflict, len(existing), path)
	}
	return svc.Create(ctx, tx, in)
}

// compile-time proof that the service is the two things the kernel wires.
var (
	_ httpx.TenantLoader = (contracts.Service)(nil)
	_ interface {
		List(context.Context, db.Tx[db.System]) ([]tenancy.Tenant, error)
	} = contracts.Active{}
)
