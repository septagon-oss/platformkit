package change_test

// The door this module owns, asked of the module that owns it.
//
// `modules/change` claims four things about its `Gate`, and the claims are the
// reason it exists rather than a composition writing the same eleven lines: the
// subject owner is asked *before* the switch, so a provider nobody can reach never
// refuses a write nobody protected; the switch is evaluated for the tenant the
// request resolved to, and not for the process that happens to serve it; a refusal
// names the fields and the address, because a refusal that names neither is a wall;
// and a switch that does not answer is a 503 on a protected write, which decision
// 0010 calls the opposite of a silent allow.
//
// None of that is visible from an HTTP route: every one of those sentences is about
// the order in which two questions are asked, and an end-to-end case can only
// observe the answer. So the recorder below counts the switch reads, and the
// assertions are reached through that count. One schema serves the six cases because
// each of them is about a different answer from the same two ports, and a foundation
// migrated per case would be paying for the fixture rather than the claim.

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/flags"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/change"
	"github.com/septagon-oss/platformkit/modules/change/contracts"
)

const (
	gateKey    = "change.control.test-sla"
	gateWayOn  = "/api/v1/change/proposals"
	gateModule = "testsubject"
	gateEntity = "row"
)

var gateTenant = tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}

// recorder is both questions a Gate asks, with the answers this case gave and the
// calls those answers received. One type for both because the point of this file is
// the *order* between them, and an order is only observable from the middle.
type recorder struct {
	protected  []string
	protectedE error
	asked      []contracts.DirectWrite

	on         bool
	flagE      error
	switched   []flags.Subject
	switchKeys []string
}

func (r *recorder) Protected(_ context.Context, _ db.Tx[db.Tenant], w contracts.DirectWrite) ([]string, error) {
	r.asked = append(r.asked, w)
	return r.protected, r.protectedE
}

func (r *recorder) Boolean(_ context.Context, key string, subject flags.Subject, _ bool) (flags.Decision, error) {
	r.switchKeys = append(r.switchKeys, key)
	r.switched = append(r.switched, subject)
	return flags.Decision{Value: r.on}, r.flagE
}

// gateUnder calls one Gate the way a route calls it — with the caller's context and
// the caller's transaction, nothing handed in by hand — beneath a tenant that the
// transaction itself resolves to.
func gateUnder(t *testing.T, conn *db.Conn, r *recorder, fn func(g change.Gate, ctx context.Context, tx db.Tx[db.Tenant])) {
	t.Helper()
	gate := change.NewGate(change.Gate{
		Flags: r, Key: gateKey, WayOn: gateWayOn,
		Subject:    contracts.SubjectRef{Module: gateModule, Entity: gateEntity},
		Protection: r,
	})
	if err := db.Run(tenancy.WithTenant(t.Context(), gateTenant), conn,
		func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			fn(gate, ctx, tx)
			return nil
		}); err != nil {
		t.Fatalf("the tenant's own transaction: %v", err)
	}
}

// update is the write every case below asks about: one field, moved by a PATCH.
func update(id uuid.UUID, changed ...string) contracts.DirectWrite {
	return contracts.DirectWrite{
		Subject: contracts.SubjectRef{Module: gateModule, Entity: gateEntity, ID: id},
		Verb:    contracts.WriteUpdate,
		Changed: changed,
	}
}

// TestTheGateAsksTheSubjectOwnerBeforeTheSwitch is the ordering the whole design
// rests on, and the four answers that follow it.
func TestTheGateAsksTheSubjectOwnerBeforeTheSwitch(t *testing.T) {
	_, conn := dbtest.Schema(t)

	// The provider is broken on purpose here: read the flag first and this write —
	// which the module just said needs no proposal — answers 503, which is decision
	// 0010's silent allow arrived as a self-inflicted outage.
	t.Run("an unreadable switch never refuses a write nobody protects", func(t *testing.T) {
		r := &recorder{protected: nil, flagE: errors.New("the provider is down")}
		gateUnder(t, conn, r, func(g change.Gate, ctx context.Context, tx db.Tx[db.Tenant]) {
			if err := g.Check(ctx, tx, update(uuid.New(), "title")); err != nil {
				t.Errorf("a write of nothing protected was refused: %v", err)
			}
		})
		if len(r.asked) != 1 {
			t.Fatalf("the subject owner was asked %d times, want once", len(r.asked))
		}
		if len(r.switched) != 0 {
			t.Errorf("an unreadable switch was read %d times for a write nobody protects; the refusal it causes is nobody's policy",
				len(r.switched))
		}
	})

	// The first half of what a "protected field" means at an installation that has
	// not asked for one — and which key, for which tenant, the answer was read for,
	// because a switch read for the wrong tenant protects one tenant's promise with
	// another tenant's value.
	t.Run("a protected field with the switch off is a write", func(t *testing.T) {
		r := &recorder{protected: []string{"slaDeadline"}, on: false}
		gateUnder(t, conn, r, func(g change.Gate, ctx context.Context, tx db.Tx[db.Tenant]) {
			if err := g.Check(ctx, tx, update(uuid.New(), "slaDeadline")); err != nil {
				t.Errorf("a protected field with the switch off = %v, want the write to proceed", err)
			}
		})
		if len(r.switchKeys) != 1 || r.switchKeys[0] != gateKey {
			t.Fatalf("the switch %v was not read exactly once under %q", r.switchKeys, gateKey)
		}
		if r.switched[0].TenantID != gateTenant.ID {
			t.Errorf("the switch was evaluated for tenant %s, want the tenant this transaction resolved to (%s)",
				r.switched[0].TenantID, gateTenant.ID)
		}
	})

	// The refusal a person acts on: which field, which address, which grant.
	t.Run("a protected write names its way through", func(t *testing.T) {
		subject := uuid.New()
		r := &recorder{protected: []string{"slaDeadline"}, on: true}
		var (
			err     error
			refusal *contracts.Refusal
		)
		gateUnder(t, conn, r, func(g change.Gate, ctx context.Context, tx db.Tx[db.Tenant]) {
			err = g.Check(ctx, tx, update(subject, "slaDeadline"))
			_ = errors.As(err, &refusal)
		})
		if !errors.Is(err, crud.ErrConflict) {
			t.Fatalf("a protected write with the switch on = %v, want it to conflict", err)
		}
		if refusal == nil {
			t.Fatalf("the conflict carries no *contracts.Refusal, so the caller was given no way through: %v", err)
		}
		if refusal.SubjectID != subject || refusal.SubjectModule != gateModule || refusal.SubjectEntity != gateEntity {
			t.Errorf("the refusal names %s/%s/%s, want the subject this door writes",
				refusal.SubjectModule, refusal.SubjectEntity, refusal.SubjectID)
		}
		if refusal.Path != gateWayOn || refusal.Permission != contracts.PermissionChangePropose {
			t.Errorf("the refusal points at %s with %q, want %s and %q",
				refusal.Path, refusal.Permission, gateWayOn, contracts.PermissionChangePropose)
		}
		if strings.Join(refusal.Fields, ",") != "slaDeadline" {
			t.Errorf("the refusal names fields %v, want the one field the answer protected", refusal.Fields)
		}
		if !strings.Contains(refusal.Error(), gateWayOn) || !strings.Contains(refusal.Error(), "slaDeadline") {
			t.Errorf("the sentence a person reads — %q — names neither the address nor the field", refusal.Error())
		}
	})

	// The half a fallback would erase: kit/flags hands the caller its fallback when
	// a provider fails, and a fallback of false here reads as "protection is off"
	// and lets the change hand itself over with no second account in sight.
	t.Run("a switch that did not answer is unavailable, not refused", func(t *testing.T) {
		r := &recorder{protected: []string{"slaDeadline"}, flagE: errors.New("provider unreachable")}
		var (
			err  error
			prob *problem.Problem
		)
		gateUnder(t, conn, r, func(g change.Gate, ctx context.Context, tx db.Tx[db.Tenant]) {
			err = g.Check(ctx, tx, update(uuid.New(), "slaDeadline"))
			_ = errors.As(err, &prob)
		})
		if prob == nil || prob.Status != http.StatusServiceUnavailable {
			t.Fatalf("an unreadable switch on a protected write = %v, want a 503", err)
		}
		if errors.Is(err, crud.ErrConflict) {
			t.Errorf("the provider's failure is dressed as a conflict the caller can correct: %v", err)
		}
		for _, want := range []string{gateKey, gateWayOn} {
			if !strings.Contains(prob.Detail, want) {
				t.Errorf("the 503 %q never names %q, so nobody reading it can tell which switch is down or where the write still goes",
					prob.Detail, want)
			}
		}
	})

	// Why the subject is the door's own: an entity that could borrow another
	// entity's answer turns one policy per row into one policy anybody may pick.
	t.Run("a gate never answers for another subject's write", func(t *testing.T) {
		r := &recorder{protected: []string{"anything"}, on: true}
		var err error
		gateUnder(t, conn, r, func(g change.Gate, ctx context.Context, tx db.Tx[db.Tenant]) {
			err = g.Check(ctx, tx, contracts.DirectWrite{
				Subject: contracts.SubjectRef{Module: "someone", Entity: "else", ID: uuid.New()},
				Verb:    contracts.WriteUpdate, Changed: []string{"anything"},
			})
		})
		if err == nil {
			t.Fatal("a write over another subject was answered rather than refused")
		}
		var refusal *contracts.Refusal
		if errors.As(err, &refusal) || errors.Is(err, crud.ErrConflict) {
			t.Errorf("a wiring bug answered as a request the caller can correct: %v", err)
		}
		if !strings.Contains(err.Error(), gateEntity) || !strings.Contains(err.Error(), "else") {
			t.Errorf("the fault %q does not name both subjects, so nobody can see which door is hung wrong", err)
		}
		if len(r.asked) != 0 {
			t.Errorf("the subject owner was asked %d times about a subject this gate does not write", len(r.asked))
		}
	})

	// The whole reason Door exists: one subject, named once, in the Gate. A
	// composition that had to spell the triple in two places would be keeping them
	// honest by hand.
	t.Run("the door hands kit/rest's write to the port", func(t *testing.T) {
		r := &recorder{protected: nil}
		id := uuid.New()
		gateUnder(t, conn, r, func(g change.Gate, ctx context.Context, tx db.Tx[db.Tenant]) {
			if err := change.Door(g).Check(ctx, tx, rest.Write{
				Module: gateModule, Entity: gateEntity, ID: id,
				Verb: rest.VerbCommand, Command: "resolve",
			}); err != nil {
				t.Errorf("the adapter refused a write its own subject owns: %v", err)
			}
		})
		if len(r.asked) != 1 {
			t.Fatalf("the port was asked %d times, want once", len(r.asked))
		}
		got := r.asked[0]
		if got.Subject.Module != gateModule || got.Subject.Entity != gateEntity || got.Subject.ID != id {
			t.Errorf("the adapter sent subject %s/%s/%s, want the Spec's own %s/%s/%s",
				got.Subject.Module, got.Subject.Entity, got.Subject.ID, gateModule, gateEntity, id)
		}
		if got.Verb != contracts.WriteCommand || got.Command != "resolve" {
			t.Errorf("the adapter sent verb %q command %q, want %q and the command's own verb",
				got.Verb, got.Command, contracts.WriteCommand)
		}
		if len(got.Changed) != 0 {
			t.Errorf("the adapter invented the field list %v for a command", got.Changed)
		}
	})
}

// TestANewGateRefusesWiringThatCannotRefuseWell keeps the three wiring mistakes that
// would produce a refusal with no way through, or a gate with no door, at boot.
func TestANewGateRefusesWiringThatCannotRefuseWell(t *testing.T) {
	whole := change.Gate{
		Key: gateKey, WayOn: gateWayOn,
		Subject:    contracts.SubjectRef{Module: gateModule, Entity: gateEntity},
		Protection: &recorder{},
	}
	for _, name := range []string{"no Protection", "no subject", "no address"} {
		g := whole
		switch name {
		case "no Protection":
			g.Protection = nil
		case "no subject":
			g.Subject = contracts.SubjectRef{Module: gateModule}
		case "no address":
			g.WayOn = ""
		}
		// A missing Flags is deliberately not on this list: Gate says an
		// installation with no provider means "protected, always", which is a
		// deployment and not a fault.
		panicked := func() (ok bool) {
			defer func() { ok = recover() != nil }()
			change.NewGate(g)
			return false
		}()
		if !panicked {
			t.Errorf("NewGate accepted a gate with %s", name)
		}
	}
}
