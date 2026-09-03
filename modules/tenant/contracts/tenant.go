// Package contracts is everything another module, an app or a test may know
// about tenants: the entity, the events, the permission and the Service
// interface. The implementation is in ../internal.
//
// A tenant is the unit of isolation: one customer, one or more hosts, and every
// row in every other table belongs to exactly one. That makes this module the
// control plane rather than a business module — the kernel resolves a request's
// host through it before the request is about anything, and the periodic jobs
// walk its list.
package contracts

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// The two states. A suspended tenant keeps its rows and stops being served: its
// hosts resolve to nothing, which reads from outside as "no site here". There
// is no third state, because "archived" and "deleted" are the same fact with
// different retention, and retention is not a lifecycle.
const (
	StatusActive    = "active"
	StatusSuspended = "suspended"
)

// Tenant is one customer of the platform.
//
// It does not embed crud.Base, and that is the whole shape of this module. Base
// contributes a tenant_id column and row-level security matches on it; a tenant
// has no tenant to belong to. So the entity is a plain struct, there is no
// rest.Spec, and the five routes are written by hand in internal/ — which is
// what an exception to a generic mechanism should cost.
type Tenant struct {
	ID     uuid.UUID `json:"id"`
	Slug   string    `json:"slug"`
	Name   string    `json:"name"`
	Status string    `json:"status"`
	// Operator says this is the installation's own tenant: the one whose
	// administrators may reach the control plane at all. Exactly one row has it,
	// the one `platformkit bootstrap` created, and no route writes it — see
	// NewTenant.
	Operator bool `json:"operator"`
	// Hosts are the names this tenant is served at, the primary one first. They
	// live in their own table and are loaded with the tenant, because a tenant
	// without its hosts is a row nobody can reach and an admin screen that
	// cannot say so.
	//
	// The order is the whole of what "primary" is for. A tenant may answer at
	// several names and every absolute URL — a mailed set-password link, a
	// notice somebody clicks from outside — has to be built on one of them.
	// Before migrations/000020 the one chosen was whichever sorted first, so
	// adding admin.acme.example.com silently moved every future link off
	// acme.example.com and nobody chose that.
	Hosts     []string   `json:"hosts" gorm:"-"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
	DeletedAt *time.Time `json:"-"`
}

// TableName pins the table, so the entity and migrations/000006 agree.
func (Tenant) TableName() string { return "tenants" }

// Tenancy is this tenant as the kernel knows it: the three fields kit/tenancy
// carries on a context. The conversion is here so that no other package decides
// which of these fields the kernel gets.
func (t *Tenant) Tenancy() tenancy.Tenant {
	return tenancy.Tenant{ID: t.ID, Slug: t.Slug, Name: t.Name, Operator: t.Operator}
}

// NewTenant is what creating one takes: a slug, a name, and the first host it
// answers at. The host is not optional, because a tenant nothing routes to
// cannot be signed into, and a create that leaves an installation unreachable
// is a create that failed quietly.
type NewTenant struct {
	Slug string `json:"slug" minLength:"2" maxLength:"63" doc:"URL-safe short name, unique across the installation" example:"acme"`
	Name string `json:"name" minLength:"1" maxLength:"200" doc:"Display name" example:"Acme Corporation"`
	Host string `json:"host" minLength:"1" maxLength:"253" doc:"The first host this tenant is served at, which is its primary one" example:"acme.example.com"`

	// Operator marks the installation's own tenant, and it is json:"-": it is
	// in no request body, in no OpenAPI schema and in no generated form, so the
	// create route cannot be asked for one however the body is written. The one
	// writer is Bootstrap, which is the one write in the application with no
	// caller to authorize and can only ever happen once.
	Operator bool `json:"-"`
}

// slugPattern is a DNS label: what a slug has to be if it is ever going to be a
// subdomain, which is what a slug is for.
var slugPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// hostPattern is a hostname: labels of letters, digits and hyphens, separated
// by dots. It is deliberately narrower than the DNS allows — no underscores, no
// trailing dot, no port — because kit/httpx has already normalised the incoming
// Host header to exactly this shape before it asks the loader.
var hostPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*$`)

// ValidSlug normalises and checks a slug.
func ValidSlug(slug string) (string, error) {
	slug = strings.ToLower(strings.TrimSpace(slug))
	if !slugPattern.MatchString(slug) {
		return "", fmt.Errorf("slug %q is not a DNS label: lower-case letters, digits and hyphens, 1 to 63 of them", slug)
	}
	return slug, nil
}

// ValidHost normalises and checks a host. Normalisation is kit/httpx's, because
// the string stored here is compared against the one the middleware derives
// from a Host header, and two normalisations that disagree is a domain that
// resolves for nobody.
func ValidHost(host string) (string, error) {
	host = strings.TrimSpace(host)
	if host == "" || len(host) > 253 || !hostPattern.MatchString(strings.ToLower(host)) {
		return "", fmt.Errorf("host %q is not a hostname", host)
	}
	return host, nil
}

// Hook is something that has to happen inside the transaction that creates a
// tenant. main hands this module a list of them.
//
// It is how a module that sits above this one is notified without this one
// importing it: the auth module seeds a new tenant's roles, and if it did so by
// subscribing to tenant.created the roles would appear whenever the worker got
// around to it — which is after the administrator that the same transaction
// created has already tried to sign in.
type Hook func(ctx context.Context, tx db.Tx[db.System], t *Tenant) error

// Inviter is how the control plane gives a tenant its first administrator
// without this module naming the one that owns people.
//
// It is declared here and satisfied in apps/platformkit over the user module,
// the way notification's RecipientLookup is: the consumer declares the
// capability it needs and the application decides who has it, so this module
// still imports nothing.
//
// The capability is narrow on purpose and it is a real one. It takes a system
// transaction, because the tenant being invited into is not the tenant the
// request resolved to — an operator is at their own host and the row belongs
// somewhere else — and there is no other way to write into a tenant from
// outside it. What it must not become is "create a user anywhere": the address
// and the tenant are the arguments, the roles are this module's decision, and
// no password crosses it, so the person invited is somebody who has to read
// their own mailbox before they are anybody at all.
type Inviter interface {
	Invite(ctx context.Context, tx db.Tx[db.System], tenantID uuid.UUID, email, displayName string) error
}

// Service is the tenant lifecycle, and the kernel's host resolution.
//
// Every command takes a db.Tx[db.System], because these rows belong to no
// tenant: a create happens before there is a tenant to scope a transaction to,
// and a list is the one query in the application that is meant to see every
// tenant there is. A module cannot mint that capability, so every caller of
// these methods was handed one — which is what makes them greppable. See
// docs/adr/0006.
//
// The errors are kit/crud's, so one mapping answers for these routes and the
// generated ones alike: crud.ErrNotFound, crud.ErrInvalid, crud.ErrConflict.
type Service interface {
	// Create writes the tenant, its first host and whatever the hooks add, in
	// one transaction. A slug or a host already taken is a conflict.
	Create(ctx context.Context, tx db.Tx[db.System], in NewTenant) (*Tenant, error)

	// AddHost gives an existing tenant another name to answer at, and says
	// whether it becomes the primary one — the host every absolute URL this
	// application builds for that tenant is built on. Adding a host the tenant
	// already answers at changes nothing unless it promotes it.
	AddHost(ctx context.Context, tx db.Tx[db.System], id uuid.UUID, host string, primary bool) (*Tenant, error)

	// Suspend stops the tenant being served. Suspending it again changes
	// nothing and publishes nothing.
	Suspend(ctx context.Context, tx db.Tx[db.System], id uuid.UUID) (*Tenant, error)

	// Get is one tenant with its hosts.
	Get(ctx context.Context, tx db.Tx[db.System], id uuid.UUID) (*Tenant, error)

	// List is every tenant that is not deleted, suspended ones included: this
	// is the control plane's own list, and a suspended tenant is precisely what
	// somebody reading it is looking for.
	List(ctx context.Context, tx db.Tx[db.System]) ([]*Tenant, error)

	// ByHost is httpx.TenantLoader. It answers tenancy.ErrNoSuchHost for a host
	// nobody serves and for a host whose tenant is suspended, because from
	// outside those are the same fact and neither is an outage.
	ByHost(ctx context.Context, tx db.Tx[db.System], host string) (tenancy.Tenant, error)

	// Hosts are the names the transaction's own tenant is served at, the
	// primary one first.
	//
	// It is the one read here that takes a tenant transaction rather than a
	// system one, and that is what it is for: a worker handling one tenant's
	// event needs the host that tenant's people reach the application at — to
	// turn a notice's path into a URL a mail client can follow — and it has no
	// business seeing anybody else's. The policy on tenant_hosts already says
	// so, so this needs no capability and grants none: the rows a tenant
	// transaction can see are that tenant's.
	//
	// It answers no error and no hosts for a tenant that has none, which cannot
	// happen through Create and can happen to a row somebody edited.
	Hosts(ctx context.Context, tx db.Tx[db.Tenant]) ([]string, error)
}

// Active adapts a Service to jobs.TenantLister: the tenants the periodic jobs
// walk, which are the ones actually being served.
//
// It is a type rather than a method because jobs.TenantLister's method is called
// List and Service.List answers a different question — every tenant, suspended
// ones included. Two questions, two names, and the adapter says which is which.
type Active struct{ Service }

// List is every tenant a job should visit.
func (a Active) List(ctx context.Context, tx db.Tx[db.System]) ([]tenancy.Tenant, error) {
	all, err := a.Service.List(ctx, tx)
	if err != nil {
		return nil, err
	}
	out := make([]tenancy.Tenant, 0, len(all))
	for _, t := range all {
		if t.Status == StatusActive {
			out = append(out, t.Tenancy())
		}
	}
	return out, nil
}
