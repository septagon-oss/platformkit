// publisher.go is one composition's own copy of the promise its modules made about
// what they emit.
//
// catalog.go keeps the process-wide record — every app this process has composed, and
// the names each answers under — because the doors that read it predate this file and a
// publish that reached no composition still has to be measured against something. What a
// record keyed by slug cannot give a caller is the answer to "which composition am I
// publishing for": one process may hold two of them (kit/appname, decision 0074 §6), so
// the record resolves that by reading tenants.app — a deployment fact the composition
// already knows. A Catalog is that fact as a value: built by the composition, carried to
// the one door every event passes through, and consulted there before the INSERT, which
// keeps the refusal inside the caller's transaction exactly where catalog.go put it.
//
// The value is a contract check and nothing else. It grants no capability: no actor,
// tenant or feature flag widens what a publish has to satisfy (decision 0028's shared
// instance), row-level security and the authorizer stay the only authority over whose
// rows the write touches, and a composition that carried another composition's Catalog
// would gain only the right to publish a name its own manifest already names.
package events

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/db"
)

// Publisher is a composition's event door: it writes the outbox row and measures the
// payload against the manifest of the composition that built it. A module that holds one
// publishes through it instead of through the package function and is measured against
// its own composition's contract wherever the publish runs; a module that does not holds
// nothing and is measured by whatever the composition installed, as before.
type Publisher interface {
	// Publish writes an event belonging to the tenant of tx.
	Publish(ctx context.Context, tx db.Tx[db.Tenant], name string, payload any) error
	// PublishFor writes an event belonging to a named tenant from a cross-tenant
	// transaction, and needs the capability that transaction is.
	PublishFor(ctx context.Context, tx db.Tx[db.System], tenantID uuid.UUID, name string, payload any) error
}

// Catalog is one composition's declared events, and the grip it holds on their names.
// It is built by the composition, is immutable afterwards, and is safe to publish
// through from any number of goroutines: readers take no lock because nothing is ever
// written into it.
type Catalog struct {
	app     appname.Name
	names   map[string]struct{}
	schemas map[string]*Schema
	// release gives back this Catalog's own grip on the process-wide record — the
	// release ClaimApp hands out, held here so disposal is one call rather than a
	// receipt the caller has to keep separately.
	release func()
}

var (
	_ Publisher = (*Catalog)(nil)
	_ measured  = (*Catalog)(nil)
)

// NewCatalog builds the event contract of one composition and claims its names, so that
// every publish this composition carries is measured against what this composition's
// modules declared rather than against whatever the process last installed under its
// slug.
//
// app is the composition's own name; the unnamed deployment passes appname.Name(""),
// which is the deployment that names no app and is published unchecked exactly as it is
// today. An app named in code that is not a valid slug is refused with appname's own
// sentence.
//
// It refuses, installing nothing and claiming nothing, when the list spells one name
// under payloads that are not one document (OneShapePerName, the sentence the manifest
// gate in kit/module reads) or when it disagrees with what that app already answers
// under (clashes, the sentence two live compositions of one app are refused with). On
// either refusal the *Catalog is nil: a composition that was refused holds no grip.
//
// An empty list is legal and means no check rather than every refusal — the manifest of a
// test composition that declares nothing, which is what DeclareAll has always installed.
func NewCatalog(app appname.Name, decls []Declared) (*Catalog, error) {
	if app.Named() {
		if _, err := appname.Parse(string(app)); err != nil {
			return nil, fmt.Errorf("events: %w", err)
		}
	}
	release, err := DeclareFor(app, decls)
	if err != nil {
		return nil, err
	}
	names := make(map[string]struct{}, len(decls))
	schemas := make(map[string]*Schema, len(decls))
	for _, d := range decls {
		names[d.Name] = struct{}{}
		// A declaration with no payload type describes nothing to check, and it is
		// present in names and absent from schemas for the same reason the record in
		// catalog.go is built that way: the name is still declared, and a nil the
		// reader has to unwrap is a member it would have to remember to skip.
		if s := d.Schema(); s != nil {
			schemas[d.Name] = s
		}
	}
	return &Catalog{app: app, names: names, schemas: schemas, release: release}, nil
}

// Publish writes an event of this composition into the outbox inside tx, measured
// against this Catalog's own manifest. The tenant comes from the transaction, never from
// the caller, and the app is the one this Catalog was built for, never the one a lookup
// would have guessed: a handler publishing under a tenant another app holds is measured
// against the contract of the composition that is serving it, and delivery still refuses
// to run that row for an app the tenant table does not name.
func (c *Catalog) Publish(ctx context.Context, tx db.Tx[db.Tenant], name string, payload any) error {
	return write(ctx, tx.DB(), db.TenantOf(tx).ID, name, payload, false, c)
}

// PublishFor is PublishFor of this composition: the payload is measured against what
// this Catalog declares, and the caller still needs the cross-tenant transaction the
// package form needs, because a tenant can only be named from a transaction entitled to
// name it.
func (c *Catalog) PublishFor(ctx context.Context, tx db.Tx[db.System], tenantID uuid.UUID, name string, payload any) error {
	if tenantID == uuid.Nil {
		return fmt.Errorf("events: %s: an event belongs to a tenant", name)
	}
	return write(ctx, tx.DB(), tenantID, name, payload, true, c)
}

// Close gives back the names this Catalog claimed, leaving standing any name a second
// live composition claimed as well. It is safe to call twice and from two goroutines.
//
// A released Catalog keeps publishing, and keeps measuring against its own manifest: the
// grip protects the *record* another publish would consult, not this one, and a publish
// that reached a Catalog whose boot has stopped is answered by the resources that boot
// released — its pool, its transport — rather than by a second refusal of its own.
func (c *Catalog) Close() { c.release() }

// measure is the two promises of this manifest, in the order the write asks them: that
// some module of this composition declared the name, and that the payload is the shape
// declared for it. Both are the sentences catalog.go already says, with this Catalog's
// app named as the owner of the contract.
func (c *Catalog) measure(name string, body []byte) error {
	if len(c.names) == 0 {
		return nil
	}
	if _, ok := c.names[name]; !ok {
		return undeclared(c.app.String(), name)
	}
	s := c.schemas[name]
	if s == nil {
		return nil
	}
	if err := s.Validate(body); err != nil {
		return notAsDeclared(c.app.String(), name, err)
	}
	return nil
}

// measured is what write asks of a Publisher it was handed: the manifest to measure the
// row against, answerable only by a value this package built. A Publisher some other
// package wrote is carried for its caller and never consulted, so the catalogue the
// composition installed answers for it as it does today and no publish can lose its
// guard by being carried rather than installed.
type measured interface {
	measure(name string, body []byte) error
}

// measureManifest is the two promises the emitting module made in its manifest, asked at
// the one door before anything is written: that the name is one somebody declared, and
// that the payload is the shape declared for it. Refused here, the state change that
// would have caused the event rolls back with the refusal.
//
// Which promise is asked depends on who is answering. A carried Catalog is the
// composition's own manifest, so it answers alone and asks nothing of the database. With
// no carried value the question "whose contract is this" is a fact about the deployment
// rather than about the caller, and the table that holds it is tenants.app: the read is
// asked only when some app typed this event — an event no app described has no contract
// to consult in any app, and no app to ask about it either, so for one of those the write
// asks the cheaper question the process answers without a read. An unchecked publish
// still costs exactly the INSERT it always cost. See catalog.go.
func measureManifest(gdb *gorm.DB, pub Publisher, tenantID uuid.UUID, name string, body []byte) error {
	if m, ok := pub.(measured); ok {
		return m.measure(name, body)
	}
	if !payloadTyped(name) {
		return checkDeclared("", name)
	}
	app, err := appOfTenant(gdb, tenantID)
	if err != nil {
		return fmt.Errorf("events: %s: %w", name, err)
	}
	if err := checkDeclared(app, name); err != nil {
		return err
	}
	return checkPayload(app, name, body)
}

// publisherKey is the one context key of this file. A composition fact reaches a
// package-level door the way the connection does on a served request (kit/httpx's
// WithConn): the door resolves it, and a caller that carried nothing is answered by the
// installed catalogue.
type publisherKey struct{}

// WithPublisher returns a context that carries p as the composition's event door. kit/app
// puts one on the context of every served request and of every job, subscription
// delivery and relay tick the worker half runs; a caller outside a composition puts one
// on a context of its own to publish under a manifest a bare publish would not be
// measured against.
func WithPublisher(ctx context.Context, p Publisher) context.Context {
	if p == nil {
		return ctx
	}
	return context.WithValue(ctx, publisherKey{}, p)
}

// PublisherFrom is the carried publisher, and whether there was one. A caller that
// passes the result to WithPublisher on a derived context hands the composition's door
// to a goroutine that would otherwise publish under context.Background().
func PublisherFrom(ctx context.Context) (Publisher, bool) {
	p, ok := ctx.Value(publisherKey{}).(Publisher)
	return p, ok
}
