package internal

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/site/contracts"
)

// The two paths: the settings an administrator reads and writes, and the site a
// visitor reads. They are siblings rather than one path with two
// authorizations, because a route is guarded by what it is and not by who asks.
const (
	settingsPath = "/api/v1/site/settings"
	publicPath   = "/api/v1/site/public"
)

var faults = []int{http.StatusUnprocessableEntity, http.StatusServiceUnavailable}

// RegisterRoutes mounts the three routes a singleton with a public face has.
//
// There is no rest.Spec: a Spec is five routes on a collection, and a tenant
// has one site. There is no POST and no DELETE either — the settings of a
// tenant are not created and not removed, so the write is a PUT that saves
// whatever is there.
func RegisterRoutes(api *httpx.API, svc contracts.Service) {
	httpx.Register(api, huma.Operation{
		OperationID: "site-settings-read",
		Method:      http.MethodGet,
		Path:        settingsPath,
		Summary:     "Read this tenant's site settings",
		Description: "A tenant that has configured nothing gets the defaults rather than a 404: every tenant has a site.",
		Tags:        []string{"site"},
		Errors:      faults,
	}, httpx.Permission(contracts.PermissionSiteManage),
		func(ctx context.Context, _ *struct{}) (*rest.Item[*contracts.SiteSettings], error) {
			tx, err := transaction(ctx)
			if err != nil {
				return nil, err
			}
			out, err := svc.Settings(ctx, tx)
			if err != nil {
				return nil, rest.Fault(err)
			}
			return &rest.Item[*contracts.SiteSettings]{Body: out}, nil
		})

	httpx.Register(api, huma.Operation{
		OperationID: "site-settings-save",
		Method:      http.MethodPut,
		Path:        settingsPath,
		Summary:     "Save this tenant's site settings",
		Description: "The whole of them: a PUT replaces what is there. Saving what is already stored publishes nothing.",
		Tags:        []string{"site"},
		Errors:      faults,
		Extensions:  map[string]any{httpx.EventsExtension: []string{contracts.EventSettingsUpdated}},
	}, httpx.Permission(contracts.PermissionSiteManage),
		func(ctx context.Context, in *saveInput) (*rest.Item[*contracts.SiteSettings], error) {
			tx, err := transaction(ctx)
			if err != nil {
				return nil, err
			}
			out, err := svc.Save(ctx, tx, in.Body)
			if err != nil {
				return nil, rest.Fault(err)
			}
			return &rest.Item[*contracts.SiteSettings]{Body: out}, nil
		})

	// What a visitor reads. It is Public because a public site is public, and
	// it is safe to be because the tenant still comes from the request's own
	// host and the query still runs under that tenant's policy.
	httpx.Register(api, huma.Operation{
		OperationID: "site-public-read",
		Method:      http.MethodGet,
		Path:        publicPath,
		Summary:     "Read this site's public settings",
		Description: "The name, the navigation and the colour scheme. Everything else a tenant configures is nobody else's business.",
		Tags:        []string{"site"},
		Errors:      []int{http.StatusNotFound, http.StatusServiceUnavailable},
	}, httpx.Public(), func(ctx context.Context, _ *struct{}) (*rest.Item[*contracts.Public], error) {
		if _, ok := tenancy.FromContext(ctx); !ok {
			return nil, problem.NotFound("no site is served at this host")
		}
		tx, err := transaction(ctx)
		if err != nil {
			return nil, err
		}
		s, err := svc.Settings(ctx, tx)
		if err != nil {
			return nil, rest.Fault(err)
		}
		nav := s.Nav
		if nav == nil {
			// A navigation is a list, and a JSON null where a list belongs is a
			// theme's null dereference. An empty site has an empty one.
			nav = contracts.Nav{}
		}
		return &rest.Item[*contracts.Public]{Body: &contracts.Public{Title: s.Title, Nav: nav, Theme: s.Theme}}, nil
	})
}

// transaction is the request's, or a 503 saying why there is none, which is
// what kit/rest answers with for the same condition.
func transaction(ctx context.Context) (db.Tx[db.Tenant], error) {
	tx, ok := httpx.TxFrom(ctx)
	if !ok {
		return tx, problem.New(http.StatusServiceUnavailable, "the database is not reachable right now")
	}
	return tx, nil
}

// saveInput is the whole settings object. The server owns the id and the
// timestamps whatever a body says about them, which is what crud.Base's
// readOnly tags declare and what Service.Save enforces.
type saveInput struct {
	Body *contracts.SiteSettings `required:"true"`
}
