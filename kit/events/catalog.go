// catalog.go is the promise a module makes about the shape of what it emits,
// and the check the outbox holds it to.
//
// A name alone is not a contract: modules/user could publish "user.invited" with
// any JSON in it and every subscriber would learn about it in production. So a
// manifest names each event with the Go type its payload is (Declared), the
// projection of that type is the schema (schema.go), and write — the one INSERT
// every event already passes through — refuses a payload that is not one. The
// catalog is not a second declaration anybody maintains: it is the manifest,
// read once per composition, and kept under the slug of the app that composed it.
//
// The refusal is inside the caller's authoritative transaction, so a mis-shaped
// event rolls back the state change that would have caused it. That is the
// behaviour change worth naming: a publisher that gets this error has a bug in
// the code that built the payload, no retry fixes it, and the mutation it came
// with does not happen. Rule 9 in one sentence.
package events

import (
	"fmt"
	"reflect"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/septagon-oss/platformkit/kit/appname"
)

// Declared is one event a module emits: its name and the Go type of its
// payload. Declare is how a manifest writes one.
type Declared struct {
	Name string
	// Payload is the type Publish receives for this event. Nil is allowed and
	// means the module emits a payload the kernel cannot describe — a hand-built
	// JSON document, a type that marshals itself. The event is then published
	// unchecked, and it is listed as uncovered in the AsyncAPI document rather
	// than pretended to be.
	Payload reflect.Type
}

// Declare names an event and the Go type of its payload:
//
//	Declared: []events.Declared{events.Declare[contracts.Invited](contracts.EventInvited)}
//
// The type argument is the only way to say it that the compiler can check: a
// module that renames its payload type finds every one of its declarations
// refused at build time, which is the moment a contract should break.
func Declare[T any](name string) Declared {
	return Declared{Name: name, Payload: reflect.TypeFor[T]()}
}

// Names returns the event names of a list, for the callers that only need the
// set — a subscription check, an error message, a comparison against what the
// routes recorded.
func Names(list []Declared) []string {
	out := make([]string, len(list))
	for i, d := range list {
		out[i] = d.Name
	}
	return out
}

// Schema is the payload's projection, or nil when Payload describes nothing.
func (d Declared) Schema() *Schema { return SchemaOf(d.Payload) }

// The catalog belongs to an app, not to a process.
//
// What shape user.invited has is a property of the composition that emits it, and
// one process may hold two of those: a server that hosts acme and academy over one
// database boots both (kit/appname, decision 0074 §6). A single map for the whole
// process is then replaced by whichever app was composed last, so academy's boot
// takes acme's payload contract out of the one door every event passes through, and
// the malformed event acme's own modules write begins to commit and to be relayed.
// So the catalog is keyed by app slug, and the entry a publish consults is the one
// of the app that holds the tenant the row belongs to — the same column the relay
// reads to decide which rows it carries (RelayApp) and a delivery reads to decide
// whose handler may run (holdsTenant). One column, three questions, one answer.
//
// It is still not per tenant or per request: what an app declares is what that app
// emits, and no tenant, actor, locale or feature flag widens or narrows the check
// another publisher meets (decision 0028's shared instance). The app segment comes
// from the composition and from tenants.app, which no route rewrites
// (migrations/000041). The pointer swap makes install safe beside concurrent
// publishes; readers take no lock, and the mutex below covers only the replace.
var catalog atomic.Pointer[catalogue]

// catalogInstall guards the read-modify-write of one app's entry, so that two
// compositions booted at once both end up in the catalog rather than only the
// slower of the two. Readers take no lock: the pointer swap is the handoff.
var catalogInstall sync.Mutex

// catalogue is every app this process has composed: each app's own name-to-schema
// map, plus the names worth asking which app a tenant belongs to.
type catalogue struct {
	byApp map[string]map[string]*Schema
	// typed is every event name at least one app gave a payload type to. Deciding
	// *whose* contract applies is a fact about the tenant and so costs one read of
	// the tenant table; a name no app typed has no contract to consult in any app,
	// which is every event a module emits as a hand-built document. The set is what
	// lets those reach their INSERT without the read.
	typed map[string]struct{}
}

// DeclareApp installs one app's declared events, so that every Publish of that
// app's tenants can check a payload against its own event's schema. kit/app calls
// it once per composition with the union of that composition's module manifests.
// It replaces what *this* app declared and nothing any other app declared: two
// boots in one process add to one another rather than erase one another.
func DeclareApp(app appname.Name, list []Declared) {
	slug := app.String()
	catalogInstall.Lock()
	defer catalogInstall.Unlock()
	next := catalogue{byApp: map[string]map[string]*Schema{}}
	if prev := catalog.Load(); prev != nil {
		for held, m := range prev.byApp {
			if held != slug {
				next.byApp[held] = m
			}
		}
	}
	own := make(map[string]*Schema, len(list))
	for _, d := range list {
		// A declaration with no payload type describes nothing to check, and the
		// name is absent from the map rather than present and nil: the only reader
		// of the map asks "is there a schema here?", and a nil it has to unwrap is
		// a member it would have to remember to skip.
		if s := d.Schema(); s != nil {
			own[d.Name] = s
		}
	}
	if len(own) > 0 {
		next.byApp[slug] = own
	}
	next.typed = make(map[string]struct{})
	for _, held := range next.byApp {
		for name := range held {
			next.typed[name] = struct{}{}
		}
	}
	catalog.Store(&next)
}

// DeclareAll installs the declared events of the deployment that names no app —
// the single-app installation, which is the case this call has always meant and
// what a test that publishes a declared name installs. A composition with a slug
// of its own calls DeclareApp with it: an app that declared through this call
// would check its own tenants against the contract of an app that is not it.
func DeclareAll(list []Declared) { DeclareApp("", list) }

// payloadTyped reports whether any app gave this event name a payload type, and
// so whether the write has to ask which app holds the tenant before it can check
// the payload against the right declaration.
func payloadTyped(name string) bool {
	c := catalog.Load()
	if c == nil {
		return false
	}
	_, ok := c.typed[name]
	return ok
}

// appOfTenant reads which app holds the tenant an event is being written for. It
// runs on the caller's own handle, which is the whole of the permission story:
// inside the tenant's transaction row-level security shows that tenant's own row
// and nothing else, and a cross-tenant write (PublishFor, the control plane) holds
// a system transaction that shows the one row it named.
//
// max() and coalesce answer the empty slug for a tenant with no row rather than no
// rows, the shape holdsTenant uses for the same reason: an event whose tenant row
// is gone or not yet visible has an answer (nobody's app) instead of an error, and
// the empty slug is the deployment that names no app rather than an absence to fail
// on.
func appOfTenant(gdb *gorm.DB, tenantID uuid.UUID) (string, error) {
	var whose string
	if err := gdb.Raw(`SELECT coalesce(max(tn.app), '') FROM tenants tn WHERE tn.id = ?`, tenantID).Row().Scan(&whose); err != nil {
		return "", fmt.Errorf("say which app holds tenant %s: %w", tenantID, err)
	}
	return whose, nil
}

// checkPayload refuses a payload that is not what the app that holds this tenant
// declared for the event. An event name that app did not declare is not refused
// here: the manifest gate in kit/app is what refuses a route that would publish
// it, and refusing twice here would only make the error harder to find.
func checkPayload(app, name string, body []byte) error {
	c := catalog.Load()
	if c == nil {
		return nil
	}
	s := c.byApp[app][name]
	if s == nil {
		return nil
	}
	if err := s.Validate(body); err != nil {
		owner := "the module"
		if app != "" {
			owner = "app " + strconv.Quote(app)
		}
		return fmt.Errorf("events: %s: the payload is not what %s declared: %w", name, owner, err)
	}
	return nil
}
