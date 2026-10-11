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
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"
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
// (migrations/000043). The pointer swap makes install safe beside concurrent
// publishes; readers take no lock, and the mutex below covers only the replace.
var catalog atomic.Pointer[catalogue]

// catalogInstall guards every change to the catalog below — an install, a claim
// and a release all read it, add to it and store a new value — so that two
// compositions booted at once both end up in the catalog rather than only the
// slower of the two. Readers take no lock: the pointer swap is the handoff.
var catalogInstall sync.Mutex

// grips counts the live claims on each app's event names, slug by slug, under
// catalogInstall. A name stands in its app's entry exactly while at least one
// claim — or one install that took no claim — names it, which is what makes the
// release of one boot stop at what that boot added: the same promise pkit/claims.go
// gives the process's record of which application runs on which database, and for
// the same reason — the damage both refuse is one boot overwriting, or taking away,
// a fact another boot is answering under.
var grips = map[string]map[string]int{}

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
	// declared is every name each app named in its manifest, typed or not — the
	// set byApp cannot carry, because byApp holds only what there is to check a
	// payload against and a name with no payload type has nothing to check. It is
	// what checkDeclared asks whether anybody declared the name at all.
	declared map[string]map[string]struct{}
}

// DeclareApp installs one app's declared events, so that every Publish of that
// app's tenants can check a payload against its own event's schema. kit/app calls
// it once per composition with the union of that composition's module manifests.
// It replaces what *this* app declared and nothing any other app declared: two
// boots in one process add to one another rather than erase one another.
//
// It takes no grip, and it ends the grips this process held on that app's names:
// what it installs is the whole of one app's declaration, so the claims a replaced
// composition took have nothing left to give back. A boot that has to be able to
// hand its own declarations back claims instead — ClaimApp — which is what kit/app's
// Start does, above the connection.
func DeclareApp(app appname.Name, list []Declared) {
	slug := app.String()
	catalogInstall.Lock()
	defer catalogInstall.Unlock()
	next := catalogue{byApp: map[string]map[string]*Schema{}, declared: map[string]map[string]struct{}{}}
	if prev := catalog.Load(); prev != nil {
		for held, m := range prev.byApp {
			if held != slug {
				next.byApp[held] = m
			}
		}
		for held, names := range prev.declared {
			if held != slug {
				next.declared[held] = names
			}
		}
	}
	own := make(map[string]*Schema, len(list))
	names := make(map[string]struct{}, len(list))
	for _, d := range list {
		names[d.Name] = struct{}{}
		// A declaration with no payload type describes nothing to check, and the
		// name is absent from the map rather than present and nil: the only reader
		// of the map asks "is there a schema here?", and a nil it has to unwrap is
		// a member it would have to remember to skip. It is still declared, and it
		// is `names` that says so.
		if s := d.Schema(); s != nil {
			own[d.Name] = s
		}
	}
	if len(own) > 0 {
		next.byApp[slug] = own
	}
	if len(names) > 0 {
		next.declared[slug] = names
	}
	next.typed = make(map[string]struct{})
	for _, held := range next.byApp {
		for name := range held {
			next.typed[name] = struct{}{}
		}
	}
	delete(grips, slug)
	catalog.Store(&next)
}

// DeclareAll installs the declared events of the deployment that names no app —
// the single-app installation, which is the case this call has always meant and
// what a test that publishes a declared name installs. A composition with a slug
// of its own calls DeclareApp with it: an app that declared through this call
// would check its own tenants against the contract of an app that is not it.
func DeclareAll(list []Declared) { DeclareApp("", list) }

// ClaimApp installs one app's declared events beside the ones that app already
// holds and returns the release of what it added. It refuses, installing nothing,
// when the list spells an event that app already answers under a different payload
// shape: one app holds one shape per event name, so the shapes a live composition
// is answering publish under are neither replaced on the way in nor taken away on
// the way out, and a second composition of the same app that means something else
// by a name the first chose is the disagreement its author has to settle. Two
// *different* apps may spell one name two ways, because each is measured against
// its own entry and its tenants' own row says which entry that is.
//
// kit/app's Start takes the claim above the connection, as the last answer a boot
// gives before it opens anything, and releases it on every refusal after it — a
// claim that is refused has taken nothing, and a claim that is given back leaves
// another live composition's shapes standing.
//
// The returned release is safe to call twice and from two goroutines; a boot that
// never installed anything returns a release a caller may call anyway.
func ClaimApp(app appname.Name, list []Declared) (func(), error) {
	slug := app.String()
	want, err := declaredSchemas(list)
	if err != nil {
		return nil, err
	}
	catalogInstall.Lock()
	defer catalogInstall.Unlock()
	if err := clashes(slug, want); err != nil {
		return nil, err
	}
	next := standing()
	entry := maps.Clone(next.byApp[slug])
	if entry == nil {
		entry = map[string]*Schema{}
	}
	held := grips[slug]
	if held == nil {
		held = map[string]int{}
		grips[slug] = held
	}
	claim := make([]string, 0, len(want))
	declared := maps.Clone(next.declared[slug])
	if declared == nil {
		declared = map[string]struct{}{}
	}
	for name, s := range want {
		// Every declared name is gripped, typed or not: the release hands back the
		// declaration, and a declaration of a name with no payload type is a
		// declaration — it is what keeps that name published rather than refused.
		held[name]++
		declared[name] = struct{}{}
		claim = append(claim, name)
		if s == nil {
			// Nothing to check: an event this app declares without a payload type
			// stays unchecked whichever way it got here.
			continue
		}
		entry[name] = s
	}
	if len(entry) > 0 {
		next.byApp[slug] = entry
	}
	if len(declared) > 0 {
		next.declared[slug] = declared
	}
	retype(&next)
	catalog.Store(&next)
	var once sync.Once
	return func() {
		once.Do(func() {
			catalogInstall.Lock()
			defer catalogInstall.Unlock()
			left := standing()
			entry := maps.Clone(left.byApp[slug])
			heldNames := maps.Clone(left.declared[slug])
			counts := grips[slug]
			for _, name := range claim {
				if counts != nil && counts[name] > 0 {
					counts[name]--
					if counts[name] > 0 {
						continue
					}
					delete(counts, name)
				}
				delete(entry, name)
				delete(heldNames, name)
			}
			if len(entry) == 0 {
				delete(left.byApp, slug)
			} else {
				left.byApp[slug] = entry
			}
			if len(heldNames) == 0 {
				delete(left.declared, slug)
			} else {
				left.declared[slug] = heldNames
			}
			retype(&left)
			catalog.Store(&left)
		})
	}, nil
}

// DeclareMore is ClaimApp for the deployment that names no app, the slug a
// composition without an app of its own answers under.
func DeclareMore(list []Declared) (func(), error) { return ClaimApp("", list) }

// CheckAppDeclared answers the refusal ClaimApp would give, over the declarations
// standing, and changes nothing. kit/app's New asks, so a composition that spells
// one of its own app's event names another way is refused with its deployment still
// undialed rather than after the pool, the migration and the transport. A list that
// disagrees with itself is refused here too, by the same sentence: the promise is
// one shape per name, and nothing that reads the list gets to pick which spelling
// won.
func CheckAppDeclared(app appname.Name, list []Declared) error {
	want, err := declaredSchemas(list)
	if err != nil {
		return err
	}
	catalogInstall.Lock()
	defer catalogInstall.Unlock()
	return clashes(app.String(), want)
}

// CheckDeclared is CheckAppDeclared for the deployment that names no app.
func CheckDeclared(list []Declared) error { return CheckAppDeclared("", list) }

// declaredSchemas is one composition's list, name to projection, in the shape the
// catalog and the counts work in, and the refusal of a list that spells one name two
// ways. Taking the first spelling would be the catalog choosing which promise a
// composition keeps, which is the choice this package exists to make impossible; the
// disagreement comes back as an error instead, from the same OneShapePerName the
// manifest gate in kit/module reads, so one defect is refused in one sentence.
func declaredSchemas(list []Declared) (map[string]*Schema, error) {
	var errs []error
	for _, problem := range OneShapePerName(list) {
		errs = append(errs, errors.New("events: "+problem))
	}
	m := make(map[string]*Schema, len(list))
	for _, d := range list {
		if _, seen := m[d.Name]; !seen {
			m[d.Name] = d.Schema()
		}
	}
	return m, errors.Join(errs...)
}

// OneShapePerName names every event a list declares twice under payloads that are
// not one document, each with the shapes it was given, in the order the list named
// them: "integer then string" and "string then integer" are two different mistakes.
//
// It is the same promise clashes keeps between two compositions of one app, made
// within one list, and it is the promise the manifest gate in kit/module reads: one
// event has one payload shape, so a manifest that writes one name twice has written
// two promises, and the composition that carries it is the thing that is wrong — not
// a list a caller could clean up by taking the first entry. Two spellings of one
// document are one event (sameShape compares the projection, not the Go type), and
// an event name declared with no payload type twice is one unchecked promise, so an
// honest manifest says the same thing twice and passes.
func OneShapePerName(list []Declared) []string {
	shapes := map[string][]*Schema{}
	var order []string
	for _, d := range list {
		if _, seen := shapes[d.Name]; !seen {
			order = append(order, d.Name)
		}
		shapes[d.Name] = append(shapes[d.Name], d.Schema())
	}
	var bad []string
	for _, name := range order {
		var distinct []*Schema
		for _, s := range shapes[name] {
			known := false
			for _, k := range distinct {
				if sameShape(k, s) {
					known = true
					break
				}
			}
			if !known {
				distinct = append(distinct, s)
			}
		}
		if len(distinct) < 2 {
			continue
		}
		texts := make([]string, 0, len(distinct))
		for _, s := range distinct {
			texts = append(texts, shapeText(s))
		}
		bad = append(bad, fmt.Sprintf("%s: declared %d times under %d payloads that are not one document (%s); one event has one payload shape, so the manifest that wrote it declares one shape or names two events",
			name, len(shapes[name]), len(distinct), strings.Join(texts, ", ")))
	}
	return bad
}

// standing is a copy of what is installed, for the caller about to change it.
// Every stored catalogue is read without a lock, so no change is ever made in place.
func standing() catalogue {
	out := catalogue{byApp: map[string]map[string]*Schema{}, declared: map[string]map[string]struct{}{}}
	if c := catalog.Load(); c != nil {
		for slug, m := range c.byApp {
			out.byApp[slug] = maps.Clone(m)
		}
		for slug, names := range c.declared {
			out.declared[slug] = maps.Clone(names)
		}
	}
	retype(&out)
	return out
}

// retype rebuilds the set of names worth asking about, from the entries it is
// given. It is the one place that answer is worked out, so an install and a release
// cannot disagree about whether a payload is checked.
func retype(c *catalogue) {
	c.typed = map[string]struct{}{}
	for _, held := range c.byApp {
		for name := range held {
			c.typed[name] = struct{}{}
		}
	}
}

// clashes refuses every name in want that this app already answers under a
// different shape, each with its own sentence, in name order so the same two
// compositions refuse the same way twice.
func clashes(slug string, want map[string]*Schema) error {
	entry := map[string]*Schema{}
	if c := catalog.Load(); c != nil {
		entry = c.byApp[slug]
	}
	var errs []error
	for _, name := range slices.Sorted(maps.Keys(want)) {
		declared, ok := entry[name]
		if !ok || sameShape(declared, want[name]) {
			continue
		}
		owner := "this deployment"
		if slug != "" {
			owner = "app " + strconv.Quote(slug)
		}
		errs = append(errs, fmt.Errorf("events: %s: %s answers it under %s and this composition declares %s; one app holds one shape per event name, so the composition already standing is the one a second agrees with or is refused beside it",
			name, owner, shapeText(declared), shapeText(want[name])))
	}
	return errors.Join(errs...)
}

// sameShape reports whether two declarations of one name describe one payload. The
// projection is compared, not the Go type: two modules with their own struct for
// the same document are one event, and refusing them would refuse the composition
// for a coincidence of naming. A declared type and no declared type are never one
// shape — a name one composition leaves unchecked and another describes is exactly
// the disagreement the refusal is about.
func sameShape(a, b *Schema) bool {
	if a == nil || b == nil {
		return a == b
	}
	standing, err := json.Marshal(a)
	if err != nil {
		return false
	}
	arriving, err := json.Marshal(b)
	return err == nil && string(standing) == string(arriving)
}

// shapeText is a schema in the refusal's sentence: what the payload has to be, or
// the honest statement that nothing was declared.
func shapeText(s *Schema) string {
	if s == nil {
		return "no payload type"
	}
	b, err := json.Marshal(s)
	if err != nil {
		return "a payload shape no document describes"
	}
	return string(b)
}

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
// here — that question is asked beside this one, by checkDeclared.
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

// checkDeclared refuses a publication no manifest declares, at the one door every
// publication goes through.
//
// The manifest gate in kit/app covers routes, which is narrower than the process:
// a name published from a job, a command or a handler is not a route, and it used
// to be accepted, written, stamped published and received by nobody — the core
// review of 2026-09-29 (P2) measured two publications accepted and one audit row,
// and the loss was silent. SubscribeAll expands into one subscription per declared
// name, so an undeclared name has no subscriber by construction: the event is not
// lost later, it is never delivered at all. Refusing here says the same fact at the
// moment it becomes known, inside the caller's own transaction, so the state change
// that would have caused it rolls back, nothing is stamped and nothing is emitted.
// It is correctable in one line — the emitting module names the event in its own
// Declared list — and a retry without that line changes nothing, which is what makes
// it a refusal rather than a failure.
//
// Whose declaration it asks for is the same answer checkPayload gives, at the same
// price. For a name some app gave a payload type, the write has already read which
// app holds the tenant, so the name is asked of that app alone: a name only another
// app declares has no handler in this one either. For a name no app typed there is
// no read to ride on, and the check stops at the cheaper question the process can
// answer without one — did any app in this process declare it — so the app that
// declared it untyped still publishes it unchecked, which is the shape main's
// typed set already protects. "Declared by another app, and this app never named
// it" is therefore the one hole that costs nothing to leave open; a delivery of an
// event no handler of its own app subscribes to is silent, and naming it would cost
// every unchecked publish a read of the tenant table.
//
// The check is exactly as wide as the process's own knowledge: no catalog installed
// (DeclareApp never ran), and a composition that declared nothing, are one state
// with no names to consult, and both mean no check rather than a refusal of every
// hand-named event in a process with no composition.
func checkDeclared(app, name string) error {
	c := catalog.Load()
	if c == nil || len(c.declared) == 0 {
		return nil
	}
	if _, typed := c.typed[name]; typed {
		if _, ok := c.declared[app][name]; ok {
			return nil
		}
	} else {
		for _, names := range c.declared {
			if _, ok := names[name]; ok {
				return nil
			}
		}
	}
	owner := "any module"
	if app != "" {
		owner = "app " + strconv.Quote(app)
	}
	return fmt.Errorf("events: %s is declared by no module of %s: no subscriber can receive it", name, owner)
}
