package httpx

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// recordingAdapter is where every registration made through this API is seen:
// huma.Register and any raw handler alike end at Adapter.Handle, hidden or not.
// While builtin is set, huma is mounting its own documentation routes and the
// recorder declares them Public.
type recordingAdapter struct {
	huma.Adapter
	api     *API
	builtin bool
}

func (r *recordingAdapter) Handle(op *huma.Operation, handler func(huma.Context)) {
	if r.builtin && op != nil {
		declare(op, Public())
		if op.Path == "/openapi.json" {
			handler = r.api.integerDocumentation(handler)
		}
	}
	// The document says what the middleware does, on the same operation both of them
	// read: a POST or PATCH that can be replayed declares the header that makes it
	// replayable, and a client reading the document learns the option from the place
	// it is already reading the request's other parameters.
	if declaresKey(op) {
		declareKey(op)
	}
	r.api.record(op)
	r.Adapter.Handle(op, handler)
}

// record stores the pointer the adapter was handed, not a copy. That is the
// property the scheme rests on: boot validation and the request-time middleware
// read the same object, so a declaration cannot be visible to one and missing
// from the other.
func (a *API) record(op *huma.Operation) {
	if op == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.ops = append(a.ops, op)
}

// Recorded returns every operation the adapter has handled, in registration
// order, hidden ones included.
func (a *API) Recorded() []*huma.Operation {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.ops)
}

// ValidateDeclarations names every operation that does not carry an
// authorization this package minted. kit/app calls it once every route is
// registered and refuses to serve when it returns anything; the request-time
// middleware denies the same operations, so this turns a 403 nobody notices
// into a startup failure someone has to fix.
func (a *API) ValidateDeclarations() error {
	a.integerDocuments()
	// Huma mounts a route of its own — /schemas/{schema}, which a problem
	// document's $schema points at — and it was not mounted through a router, so
	// nothing stamped its surface. The address is the answer, and it is the same
	// function the request chain runs: an operation that reaches the document
	// without saying where it lives is stated here rather than left unstated.
	for _, op := range a.Recorded() {
		if op.Extensions == nil {
			op.Extensions = map[string]any{}
		}
		if _, ok := op.Extensions[SurfaceExtension]; !ok {
			op.Extensions[SurfaceExtension] = string(a.surfaceOf(op.Path))
		}
	}
	var bad, unanswerable, uncountable []string
	for _, op := range a.Recorded() {
		auth, ok := declarationOf(op)
		if !ok {
			bad = append(bad, describe(op))
			continue
		}
		if auth.Feature() != "" && a.opts.Entitle == nil {
			unanswerable = append(unanswerable, describe(op)+" needs the "+auth.Feature()+" feature")
		}
		if why := checkWriteLimits(describe(op), op); why != "" {
			uncountable = append(uncountable, why)
		}
	}
	if len(uncountable) > 0 {
		sort.Strings(uncountable)
		return fmt.Errorf("httpx: %d operation(s) declare a counted write this kernel cannot count:\n  %s",
			len(uncountable), strings.Join(uncountable, "\n  "))
	}
	if len(unanswerable) > 0 {
		sort.Strings(unanswerable)
		return fmt.Errorf("httpx: %d operation(s) declare a plan feature and Options.Entitle is nil:\n  %s",
			len(unanswerable), strings.Join(unanswerable, "\n  "))
	}
	if len(bad) == 0 {
		// The surface gate runs last and only when nothing else failed, because
		// a route that was never declared has no surface either and the two
		// messages together would read as two faults where one was made.
		if refusals := a.validateSurfaces(); len(refusals) > 0 {
			return fmt.Errorf("httpx: %d route(s) contradict the router they are mounted on:\n  %s",
				len(refusals), strings.Join(refusals, "\n  "))
		}
		if why := CheckRowHints(a.Resources()); why != "" {
			return fmt.Errorf("httpx: %s", why)
		}
		return nil
	}
	sort.Strings(bad)
	return fmt.Errorf("httpx: %d operation(s) do not declare their authorization; register them with httpx.Register:\n  %s",
		len(bad), strings.Join(bad, "\n  "))
}

// Required lists, once each, every grant a recorded operation asks for, with
// the operator flag the route declared. kit/app checks it against the
// manifests, so a permission nobody defines — or one the two sides disagree
// about — fails startup rather than denying everyone forever or, worse, letting
// a customer's wildcard through the control plane.
func (a *API) Required() []tenancy.Grant {
	seen := map[tenancy.Grant]bool{}
	var out []tenancy.Grant
	for _, op := range a.Recorded() {
		auth, ok := declarationOf(op)
		if !ok {
			continue
		}
		g, asks := auth.grant()
		if !asks || seen[g] {
			continue
		}
		seen[g] = true
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Permission < out[j].Permission })
	return out
}

// Declare records every permission the composition defines. kit/app calls it
// once, from the manifests, before any module registers its routes.
//
// It is here rather than in a module's Deps because the one module that needs
// the list — auth, which refuses a role naming a permission nobody defines —
// would otherwise be handed what every other module declares, and a module that
// knows the catalogue knows its neighbours. Module.Routes is given this API,
// which is the one moment the kernel has the whole list.
func (a *API) Declare(grants []tenancy.Grant) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.declared = slices.Clone(grants)
	sort.Slice(a.declared, func(i, j int) bool { return a.declared[i].Permission < a.declared[j].Permission })
}

// Permissions is every permission the composition defines, in name order. A
// module validating a list somebody typed asks this, not its neighbours.
func (a *API) Permissions() []tenancy.Grant {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.declared)
}

// EventsExtension is the OpenAPI extension an operation lists the events its
// handler publishes under. kit/rest writes it when it mounts a Spec, and
// kit/app reads it back to check that some module declared each one — the same
// recording, the same object and the same gate as the authorization
// declaration, rather than a second channel to keep in step.
const EventsExtension = "x-platformkit-events"

// Events lists, once each, every event a recorded operation says it publishes.
func (a *API) Events() []string {
	seen := map[string]bool{}
	var out []string
	for _, op := range a.Recorded() {
		if op.Extensions == nil {
			continue
		}
		names, _ := op.Extensions[EventsExtension].([]string)
		for _, n := range names {
			if !seen[n] {
				seen[n] = true
				out = append(out, n)
			}
		}
	}
	sort.Strings(out)
	return out
}

func describe(op *huma.Operation) string {
	if op.OperationID == "" {
		return op.Method + " " + op.Path
	}
	return op.Method + " " + op.Path + " (" + op.OperationID + ")"
}
