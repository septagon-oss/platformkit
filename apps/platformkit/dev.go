// Deleted in E3.
//
// This file is the tenancy and the identity the application needs before the
// tenant and auth modules exist: a static host-to-tenant map from the `dev:`
// block of config.yaml, one principal every caller is, and an authorizer that
// says yes. It is what makes `make run` boot on an empty database with nothing
// to seed, and it is the entire reason the five commands in README.md are five.
//
// It is not a shim that will quietly survive: E3 replaces every method below
// with a module that reads the tenants and the sessions out of the database,
// and this file and kit/config's Dev go with it. Until then kit/config refuses
// dev.enabled anywhere but a local host, because everything here is an
// administrator of every tenant.
package main

import (
	"context"
	"net"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// dev is the three answers the kernel cannot give itself, plus the tenant list
// the periodic jobs walk. It implements httpx.TenantLoader, httpx.Authorizer
// and jobs.TenantLister, which is exactly the set E3's two modules take over.
type dev struct {
	byHost    map[string]tenancy.Tenant
	all       []tenancy.Tenant
	principal httpx.Principal
}

// newDev reads the block. kit/config has already refused a non-local public
// host and checked that every id parses, so the errors here are unreachable —
// which is the point of validating in one place.
func newDev(cfg config.Config) (*dev, error) {
	d := &dev{byHost: map[string]tenancy.Tenant{}}
	user, err := uuid.Parse(cfg.Dev.Principal.UserID)
	if err != nil {
		return nil, err
	}
	d.principal = httpx.Principal{UserID: user, Roles: cfg.Dev.Principal.Roles}
	for _, t := range cfg.Dev.Tenants {
		id, err := uuid.Parse(t.ID)
		if err != nil {
			return nil, err
		}
		tenant := tenancy.Tenant{ID: id, Slug: t.Slug, Name: t.Name}
		d.byHost[hostOnly(t.Host)] = tenant
		d.all = append(d.all, tenant)
	}
	return d, nil
}

// ByHost is httpx.TenantLoader. A host that is not in the block is
// ErrNoSuchHost, which the HTTP layer turns into a 404 — the same answer the
// tenant module will give for a domain nobody has registered.
func (d *dev) ByHost(_ context.Context, _ db.Tx[db.System], host string) (tenancy.Tenant, error) {
	t, ok := d.byHost[host]
	if !ok {
		return tenancy.Tenant{}, tenancy.ErrNoSuchHost
	}
	return t, nil
}

// List is jobs.TenantLister: every tenant, for the jobs that walk them.
func (d *dev) List(context.Context, db.Tx[db.System]) ([]tenancy.Tenant, error) { return d.all, nil }

// Allowed is httpx.Authorizer, and it says yes to everything. That is the
// dangerous half of this file and the reason for the localhost rule.
func (d *dev) Allowed(context.Context, tenancy.Tenant, string) (bool, error) { return true, nil }

// Authenticate is the identity hook. It runs before routing, so it resolves the
// host itself: a principal has to belong to the tenant the request will resolve
// to, or kit/httpx refuses it as a session for somebody else's site.
func (d *dev) Authenticate(r *http.Request) (httpx.Principal, bool) {
	t, ok := d.byHost[hostOnly(r.Host)]
	if !ok {
		return httpx.Principal{}, false
	}
	p := d.principal
	p.TenantID = t.ID
	return p, true
}

// hostOnly is the Host header without its port, lower-cased. kit/httpx
// normalises the same way before it calls ByHost; this file needs its own copy
// because Authenticate runs before that, on the raw request.
func hostOnly(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return strings.TrimSuffix(strings.ToLower(host), ".")
}
