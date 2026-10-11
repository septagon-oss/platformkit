package change

// This file is the reusable answer a door wants: given a write, say whether it is
// refused, and refuse it with the one sentence that says where the write goes
// instead. It lives in the manifest package and not in contracts/ because it is a
// thing to be *called* at a route, not a thing another module names in its own
// signature: contracts/ stays free of flags and problem documents the way it is
// free of drivers.
//
// Two decisions are worth reading before the code:
//
// The switch is read *after* the module answers. A write that touches nothing
// protectable is a write, and reading the flag first would make an unreachable
// provider refuse writes nobody had protected — decision 0010's silent allow,
// inverted into a self-inflicted outage. So the empty answer costs nothing, and the
// only request that ever sees CHANGE_CONTROL_UNAVAILABLE is one that was about to
// change a protected field.
//
// The subject triple is the gate's own and not the caller's. A gate that trusted a
// write's self-declared module would let one entity borrow another's answer, which
// is the difference between one policy per entity and one policy that anybody can
// pick.

import (
	"context"
	"fmt"
	"net/http"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/flags"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/change/contracts"
)

// Gate is a composition's answer at one door over one subject: the field
// vocabulary it asked Protection about, the switch that says whether protection is
// on for this installation, and the address a refused write goes to instead.
//
// Every choice it holds was made once, at composition, and none of them is
// answerable from a request: Check takes the caller's transaction and their own
// identity out of the context, so the tenant the switch is evaluated for is the
// tenant the request resolved to (flags.Subject carries it, the way the site
// settings gate already passes it). Nothing here is per-process state a shared
// instance would have to unpick.
type Gate struct {
	// Flags is the installation's switch. Nil means no switch: a protected field is
	// protected, always, which is what an installation without a flag provider means
	// when it wires a gate at all.
	Flags flags.Evaluator
	// Key names the switch. This module names no key of its own: choosing one is a
	// fact about an installation, and the reference application's is not another's.
	Key string
	// WayOn is where the refusal points — the proposals collection as this
	// application serves it. modules/change owns the door and must not name the
	// address, which is why this is supplied rather than known.
	WayOn string
	// Subject is what the door writes. It is the gate's own, and Write values that
	// disagree with it are a wiring bug, not a request.
	Subject contracts.SubjectRef
	// Protection answers which of the changed fields are protected. Nil means
	// nothing anywhere is, which is the same as wiring no gate at all and is
	// refused at construction for being pointless.
	Protection contracts.Protection
}

// NewGate checks the wiring where it is written, the way every other refusal in
// this package works: a gate with no door to point at would refuse a write and
// leave the caller with no way through it.
func NewGate(g Gate) Gate {
	switch {
	case g.Protection == nil:
		panic("change: a Gate with no Protection protects nothing; wire no gate instead")
	case g.Subject.Module == "" || g.Subject.Entity == "":
		panic("change: a Gate has to name the subject its door writes")
	case g.WayOn == "":
		panic("change: a refusal has to name the address the write goes through instead")
	}
	return g
}

// Check is the question one door asks. A non-nil error means nothing was written.
//
// The refusals, in the order they are reached:
//
// crud.ErrConflict wrapping *contracts.Refusal — the write changes a protected
// field and the switch is on. Correctable, and the refusal names the fields, the
// address and the permission.
//
// A 503 problem — the switch did not answer, on a write that touches a protected
// field. Nobody in the request can fix that, and the proposal door reads no flag,
// so the write can still be put forward while the provider is down.
//
// A 500 — the write names a subject this gate does not write. Neither correctable
// nor a condition of the request: a composition bug, and it has to be loud.
func (g Gate) Check(ctx context.Context, tx db.Tx[db.Tenant], w contracts.DirectWrite) error {
	if w.Subject.Module != g.Subject.Module || w.Subject.Entity != g.Subject.Entity {
		// The subject comes from the door's own Spec, so the only way they differ is
		// a composition that hung one subject's answer on another entity's writes.
		// Loud, and not a status a client can act on.
		return fmt.Errorf("change: this door protects %s/%s and was asked about a write over %s/%s",
			g.Subject.Module, g.Subject.Entity, w.Subject.Module, w.Subject.Entity)
	}
	protected, err := g.Protection.Protected(ctx, tx, w)
	if err != nil {
		return err
	}
	if len(protected) == 0 {
		// Nothing here is protected, so nothing here needs a switch. Asked and
		// answered beneath the flag read, which is the whole point of the ordering.
		return nil
	}
	on, err := g.on(ctx, tx)
	if err != nil {
		return err
	}
	if !on {
		return nil
	}
	return fmt.Errorf("%w: %w", crud.ErrConflict, &contracts.Refusal{
		SubjectModule: w.Subject.Module, SubjectEntity: w.Subject.Entity, SubjectID: w.Subject.ID,
		Path: g.WayOn, Permission: contracts.PermissionChangePropose,
		Fields: protected,
	})
}

// Door is a Gate as kit/rest sees it: the same decision, mounted on a Spec's write
// routes. The subject of the write comes from the Spec — its module, its entity and
// the row the kernel just locked — so a composition names the subject once, in the
// Gate, instead of spelling it twice and hoping the two agree.
//
// It is a function and not a method on Gate because rest.Gate is an interface this
// package satisfies for somebody else's route, and a method would put kit/rest in
// the type that answers the question this module owns.
func Door(g Gate) rest.Gate { return door{g} }

type door struct{ g Gate }

func (d door) Check(ctx context.Context, tx db.Tx[db.Tenant], w rest.Write) error {
	return d.g.Check(ctx, tx, contracts.DirectWrite{
		Subject: contracts.SubjectRef{Module: w.Module, Entity: w.Entity, ID: w.ID},
		Verb:    w.Verb, Command: w.Command, Changed: w.Changed,
	})
}

// on reads the switch for the tenant and actor this request resolved to. An
// evaluation error is not "off": kit/flags hands the caller its fallback when a
// provider fails, and the fallback here is false, so a provider that is
// unreachable would read as a switch that is off and change hands with no second
// account in sight. That is decision 0010's silent allow and it is refused.
func (g Gate) on(ctx context.Context, tx db.Tx[db.Tenant]) (bool, error) {
	if g.Flags == nil {
		return true, nil
	}
	actor, _ := tenancy.ActorFrom(ctx)
	decision, err := g.Flags.Boolean(ctx, g.Key,
		flags.Subject{TenantID: db.TenantOf(tx).ID, TargetingKey: actor.String()}, false)
	if err != nil {
		return false, flagUnreadable(g.Key, g.WayOn)
	}
	return decision.Value, nil
}

// flagUnreadable is the door's answer when the switch could not be read at all. It
// builds a fresh Problem per call, because kit/httpx's response transformer writes
// the request id into the Problem a handler returns, and one shared value would be
// two requests writing one field at once.
func flagUnreadable(key, wayOn string) error {
	return problem.New(http.StatusServiceUnavailable,
		"CHANGE_CONTROL_UNAVAILABLE: "+key+" did not answer, so this write is refused; "+
			wayOn+" is decided by a second account whether or not that switch is reachable")
}
