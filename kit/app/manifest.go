package app

import (
	"fmt"
	"sort"
	"strings"

	"github.com/septagon-oss/platformkit/kit/appname"

	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
)

// routeKind names the constructor a route used, for the one error that has to
// say which of the two sides is wrong.
var routeKind = map[bool]string{false: "httpx.Permission", true: "httpx.OperatorPermission"}

// validatePermissions is the declaration gate's mirror, and it checks two
// things about the same list.
//
// Every permission a route requires has to be one some module defines, or the
// route is guarded by a token no role can ever be granted. And the route and
// the manifest have to agree about whether it is the operator's: a
// control-plane route declared with httpx.Permission is reachable by every
// customer's administrator through the wildcard they hold in their own tenant,
// and an ordinary route declared with httpx.OperatorPermission is reachable by
// nobody outside it. Both are silent in production and loud here.
//
// Every violation is reported at once, because a composition is fixed once.
func validatePermissions(api *httpx.API, mods []module.Module) error {
	kind := map[string]bool{}
	defined := map[string]bool{}
	for _, m := range mods {
		for _, p := range m.Permissions {
			defined[p.Key], kind[p.Key] = true, p.Operator
		}
	}
	var bad []string
	for _, g := range api.Required() {
		switch {
		case !defined[g.Permission]:
			bad = append(bad, fmt.Sprintf("%s guards a route and is defined by no module", g.Permission))
		case kind[g.Permission] != g.Operator:
			// httpx.Permission goes with Operator: false and
			// httpx.OperatorPermission with Operator: true; this names which
			// side is which, because either mismatch is silent in production.
			bad = append(bad, fmt.Sprintf("%s: its route declares %s and its manifest declares Operator: %v",
				g.Permission, routeKind[g.Operator], kind[g.Permission]))
		}
	}
	if len(bad) == 0 {
		return nil
	}
	sort.Strings(bad)
	return fmt.Errorf("app: %d permission(s) do not check out:\n  %s", len(bad), strings.Join(bad, "\n  "))
}

// kernelModule declares the kernel's event names and payload types for outbox
// validation, wildcard subscriptions and AsyncAPI. Ledger movement is declared
// only for named applications, matching the route and retry job that can emit it.
func kernelModule(app appname.Name) module.Module {
	declared := []events.Declared{
		events.Declare[events.OperationOutcome](events.EventOperationOutcome),
		events.Declare[events.ReplayRecord](events.EventReplayed),
		events.Declare[Denied](EventDenied),
		events.Declare[AccessRequested](EventAccessRequested),
	}
	self := module.Module{Name: module.KernelName}
	if app.Named() {
		declared = append(declared, events.Declare[events.LedgerMovedRecord](events.EventLedgerMoved))
		// The two grants the control plane's delivery verbs are guarded by, declared
		// by the same condition as the routes: kit/app refuses a route guarded by a
		// permission no manifest defines, and a manifest that defined one no route
		// uses would be a grant somebody could hand out for a door that is not there.
		self.Permissions = []module.Permission{
			{Key: PermissionLedgerMove, Operator: true, Label: "move this app's delivery ledger onto its own durable"},
			{Key: PermissionEventReplay, Operator: true, Label: "replay one of this app's events"},
		}
	}
	self.Declared = declared
	return self
}

// declaredEvents is every event every manifest declares, de-duplicated by name — the
// list once the gate above has passed, which is what the boot installs and what the
// AsyncAPI document renders and counts coverage over, each name said once.
//
// The de-duplication says nothing about which spelling stood, because after that gate
// every spelling of one name is one shape: module.Validate refuses a manifest that
// declares one name under two payloads and the namespaced-emit rule refuses two
// manifests that emit one name, so a name has one emitter and one shape or the
// composition never reached here.
func declaredEvents(mods []module.Module) []events.Declared {
	var out []events.Declared
	seen := map[string]bool{}
	for _, m := range mods {
		for _, e := range m.Emits() {
			if !seen[e.Name] {
				seen[e.Name], out = true, append(out, e)
			}
		}
	}
	return out
}

// allDeclaredEvents is every declaration every manifest wrote, in build order, with
// nothing dropped. It is the input to the catalog's gate: the disagreement it refuses
// is between two declarations of one name, and a caller that hands the gate a list
// already joined by name has answered the question before asking it.
func allDeclaredEvents(mods []module.Module) []events.Declared {
	var out []events.Declared
	for _, m := range mods {
		out = append(out, m.Emits()...)
	}
	return out
}

// validateEvents is the same gate for the other direction: an operation that
// will publish an event no module declared is an event no subscriber can be
// written against, because the manifest is where a subscriber looks.
func validateEvents(api *httpx.API, mods []module.Module) error {
	declared := map[string]bool{}
	for _, m := range mods {
		for _, e := range m.Emits() {
			declared[e.Name] = true
		}
	}
	var missing []string
	for _, e := range api.Events() {
		if !declared[e] {
			missing = append(missing, e)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("app: %d event(s) would be published by a route and are declared by no module: %s",
		len(missing), strings.Join(missing, ", "))
}
