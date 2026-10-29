package tasktest

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/porttest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/task/contracts"
)

// PolicyBehavior selects a fixture artifact or protocol response, not a runtime
// provider. ReferencePolicy permits assignment and unassigned/assignee resolution.
type PolicyBehavior string

const (
	ReferencePolicy     PolicyBehavior = "reference"
	DeniedPolicy        PolicyBehavior = "denied"
	UndefinedPolicy     PolicyBehavior = "undefined-or-false"
	MissingDecision     PolicyBehavior = "missing-decision"
	WrongDecision       PolicyBehavior = "wrong-decision"
	ConflictingDecision PolicyBehavior = "conflicting-decision"
	UnavailablePolicy   PolicyBehavior = "unavailable"
)

// PolicyFixture extends the service fixture with explicit policy replacement
// and evaluation observations. Calls returns a snapshot; Use installs a fresh
// artifact for subsequent invocations. Direct is false for HTTP fixtures.
type PolicyFixture struct {
	Fixture
	Use    func(PolicyBehavior)
	Calls  func() []tenancy.PolicyRequest
	Direct bool
}

// RunPolicy runs the same task consumer cases against each supplied adapter and
// persistence world. Each case needs fresh rows and an ordinary user principal.
// SQL rollback, row locking and HTTP reply replay require additional fixtures.
func RunPolicy(t *testing.T, h func(*testing.T, func(PolicyFixture))) {
	t.Helper()
	porttest.Run(t, porttest.Suite[PolicyFixture]{
		Port: "task policy consumer", World: h,
		Events:   func(f PolicyFixture) []string { return f.Published() },
		Classify: classify,
		Own:      policyCases(),
	})
}

func policyCases() []porttest.Case[PolicyFixture] {
	cases := []porttest.Case[PolicyFixture]{
		{Name: "assignment and its retry carry the requested assignee", Because: "assignment permits another person and retries still authorize", Run: policyAssignment},
		{Name: "unassigned and owned tasks resolve once", Because: "positive cases rule out an all-deny adapter", Run: policyResolution},
		{Name: "resource and actor changes never reuse a decision", Because: "both allow then deny and deny then allow must evaluate current scope", Run: policyResources},
		{Name: "policy precedes lifecycle conflicts", Because: "a stored terminal row is no authority to read it", Run: policyConflicts},
		{Name: "invalid assignment never evaluates", Because: "input refusal precedes external work", Run: policyInvalidAssignment},
		{Name: "another tenant cannot reach the resource", Because: "a provider allow cannot bypass tenant storage isolation", Run: policyOtherTenant},
		{Name: "missing principal is never promoted", Because: "a configured policy requires an explicit actor", Run: policyMissingActor},
		{Name: "same actor loses authority between retries", Because: "a fresh invocation must consult the replacement artifact", Run: policyRevoked},
		{Name: "retry reads committed reassignment", Because: "an outage does not freeze the resource facts for recovery", Run: policyReassigned},
	}
	for _, behavior := range []PolicyBehavior{DeniedPolicy, UndefinedPolicy, MissingDecision, WrongDecision, ConflictingDecision, UnavailablePolicy} {
		cases = append(cases, porttest.Case[PolicyFixture]{
			Name:    string(behavior) + " never falls back to a prior allow",
			Because: "provider refusal must precede the idempotent return and leave task state and events intact",
			Run: func(t *testing.T, f PolicyFixture) {
				row := f.Seed(open("policy fault"))
				f.attempt(t, f.Ctx, row, uuid.Nil, "done", nil, true, true)
				f.Use(behavior)
				want := tenancy.ErrPolicyUnavailable
				if behavior == DeniedPolicy || behavior == UndefinedPolicy {
					want = tenancy.ErrPolicyDenied
				}
				f.attempt(t, f.Ctx, row, uuid.Nil, "done", want, false, true)
			},
		})
	}
	return cases
}

func policyAssignment(t *testing.T, f PolicyFixture) {
	row, other := f.Seed(open("assignment")), uuid.New()
	got := f.attempt(t, f.Ctx, row, other, "", nil, true, true)
	if got.Status != contracts.StatusAcknowledged || got.AssigneeID == nil || *got.AssigneeID != other {
		t.Fatalf("assignment = %+v", got)
	}
	f.attempt(t, f.Ctx, row, other, "", nil, false, true)
	last := f.Calls()[len(f.Calls())-1]
	if last.Resource.Attributes["requested_assignee_id"] != other.String() || last.Resource.Attributes["assignee_id"] != other.String() {
		t.Fatalf("assignment facts = %+v", last)
	}
}

func policyResolution(t *testing.T, f PolicyFixture) {
	principal, _ := tenancy.PrincipalFrom(f.Ctx)
	for _, owner := range []*uuid.UUID{nil, &principal.UserID} {
		task := open("resolution")
		task.AssigneeID = owner
		row := f.Seed(task)
		got := f.attempt(t, f.Ctx, row, uuid.Nil, " done ", nil, true, true)
		if got.Status != contracts.StatusResolved || got.Resolution != "done" || got.ResolvedAt == nil {
			t.Fatalf("resolution = %+v", got)
		}
		if _, offset := got.ResolvedAt.Zone(); offset != 0 {
			t.Fatalf("resolution timestamp is not UTC: %v", got.ResolvedAt)
		}
		for _, text := range []string{"done", " done ", ""} {
			f.attempt(t, f.Ctx, row, uuid.Nil, text, nil, false, true)
		}
	}
}

func policyResources(t *testing.T, f PolicyFixture) {
	principal, _ := tenancy.PrincipalFrom(f.Ctx)
	other := uuid.New()
	for _, owners := range [][]uuid.UUID{{principal.UserID, other}, {other, principal.UserID}} {
		for _, owner := range owners {
			task := open("scoped resolution")
			task.AssigneeID = &owner
			row := f.Seed(task)
			var want error
			if owner != principal.UserID {
				want = tenancy.ErrPolicyDenied
			}
			f.attempt(t, f.Ctx, row, uuid.Nil, "done", want, want == nil, true)
			if owner == other {
				f.attempt(t, As(f.Ctx, other), row, uuid.Nil, "done", nil, true, true)
			}
		}
	}
}

func policyConflicts(t *testing.T, f PolicyFixture) {
	for _, status := range []string{contracts.StatusResolved, contracts.StatusClosed} {
		task := done(status)
		task.Resolution = "original"
		row := f.Seed(task)
		for _, tc := range []struct {
			mode PolicyBehavior
			want error
		}{
			{DeniedPolicy, tenancy.ErrPolicyDenied}, {UnavailablePolicy, tenancy.ErrPolicyUnavailable}, {ReferencePolicy, crud.ErrConflict},
		} {
			f.Use(tc.mode)
			f.attempt(t, f.Ctx, row, uuid.Nil, "changed", tc.want, false, true)
		}
	}
}

func policyInvalidAssignment(t *testing.T, f PolicyFixture) {
	row := f.Seed(open("invalid assignment"))
	before, _ := f.Task(row)
	events, calls := slices.Clone(f.Published()), len(f.Calls())
	got, err := f.Service.Assign(f.Ctx, f.Tx, row, uuid.Nil)
	if got != nil || !errors.Is(err, crud.ErrInvalid) {
		t.Fatalf("invalid assignment = %+v, %v", got, err)
	}
	f.unchanged(t, row, before, events)
	if len(f.Calls()) != calls {
		t.Fatal("invalid assignment reached policy")
	}
}

func policyOtherTenant(t *testing.T, f PolicyFixture) {
	row := f.Seed(open("foreign task"))
	ctx := tenancy.WithTenant(f.Ctx, tenancy.Tenant{ID: uuid.New(), Slug: "elsewhere"})
	for _, assignee := range []uuid.UUID{uuid.Nil, uuid.New()} {
		f.attempt(t, ctx, row, assignee, "done", crud.ErrNotFound, false, false)
	}
}

func policyMissingActor(t *testing.T, f PolicyFixture) {
	if !f.Direct {
		t.Skip("HTTP authentication precedes the configured-policy service")
	}
	row := f.Seed(open("explicit actor"))
	f.attempt(t, As(f.Ctx, uuid.Nil), row, uuid.Nil, "done", tenancy.ErrPolicyDenied, false, false)
}

func policyRevoked(t *testing.T, f PolicyFixture) {
	row := f.Seed(open("revocation"))
	f.attempt(t, f.Ctx, row, uuid.Nil, "done", nil, true, true)
	f.Use(DeniedPolicy)
	f.attempt(t, f.Ctx, row, uuid.Nil, "done", tenancy.ErrPolicyDenied, false, true)
}

func policyReassigned(t *testing.T, f PolicyFixture) {
	principal, _ := tenancy.PrincipalFrom(f.Ctx)
	task := open("reassigned during recovery")
	task.AssigneeID = &principal.UserID
	row, other := f.Seed(task), uuid.New()
	f.Use(UnavailablePolicy)
	f.attempt(t, f.Ctx, row, uuid.Nil, "done", tenancy.ErrPolicyUnavailable, false, true)
	f.Use(ReferencePolicy)
	f.attempt(t, f.Ctx, row, other, "", nil, true, true)
	f.attempt(t, f.Ctx, row, uuid.Nil, "done", tenancy.ErrPolicyDenied, false, true)
	last := f.Calls()[len(f.Calls())-1]
	if last.Resource.Attributes["assignee_id"] != other.String() {
		t.Fatalf("retry facts = %+v", last)
	}
}

func (f PolicyFixture) attempt(t *testing.T, ctx context.Context, row, assignee uuid.UUID, text string, want error, changed, evaluated bool) *contracts.Task {
	t.Helper()
	before, err := f.Task(row)
	if err != nil {
		t.Fatal(err)
	}
	events, calls := slices.Clone(f.Published()), len(f.Calls())
	var got *contracts.Task
	if assignee == uuid.Nil {
		got, err = f.Service.Resolve(ctx, f.Tx, row, text)
	} else {
		got, err = f.Service.Assign(ctx, f.Tx, row, assignee)
	}
	if !errors.Is(err, want) || (want != nil && got != nil) {
		t.Fatalf("command = %+v, %v; want %v", got, err, want)
	}
	wantCalls := calls
	if evaluated {
		wantCalls++
	}
	if len(f.Calls()) != wantCalls {
		t.Fatalf("evaluations = %d, want %d", len(f.Calls()), wantCalls)
	}
	if evaluated {
		f.facts(t, ctx, before, assignee)
	}
	if !changed {
		f.unchanged(t, row, before, events)
		if want == nil && !f.sameAnswer(got, before) {
			t.Fatalf("retry returned stale row: %+v; stored %+v", got, before)
		}
		return got
	}
	after, err := f.Task(row)
	if err != nil || !f.sameAnswer(got, after) || after.Revision != before.Revision+1 || after.UpdatedAt.Before(before.UpdatedAt) {
		t.Fatalf("transition = %+v; stored %+v, before %+v, error %v", got, after, before, err)
	}
	event := contracts.EventResolved
	if assignee != uuid.Nil {
		event = contracts.EventAssigned
	}
	if !slices.Equal(f.Published(), append(events, event)) {
		t.Fatalf("events = %v, want one %s", f.Published(), event)
	}
	if got.ID != row || (f.Direct && got.TenantID != before.TenantID) {
		t.Fatalf("transition changed identity: %+v", got)
	}
	return got
}

func (f PolicyFixture) unchanged(t *testing.T, row uuid.UUID, before contracts.Task, events []string) {
	t.Helper()
	after, err := f.Task(row)
	if err != nil || !reflect.DeepEqual(before, after) || !slices.Equal(events, f.Published()) {
		t.Fatalf("refusal/retry changed owner state: before %+v, after %+v, events %v -> %v, error %v", before, after, events, f.Published(), err)
	}
}

func (f PolicyFixture) facts(t *testing.T, ctx context.Context, before contracts.Task, assignee uuid.UUID) {
	t.Helper()
	got := f.Calls()[len(f.Calls())-1]
	tenant, _ := tenancy.FromContext(ctx)
	principal, _ := tenancy.PrincipalFrom(ctx)
	action := "task:resolve"
	attrs := map[string]any{"status": before.Status, "priority": before.Priority, "assignee_id": ""}
	if before.AssigneeID != nil {
		attrs["assignee_id"] = before.AssigneeID.String()
	}
	if assignee != uuid.Nil {
		action = "task:assign"
		attrs["requested_assignee_id"] = assignee.String()
	}
	if got.Tenant.ID != tenant.ID || got.Tenant.Slug != tenant.Slug || got.Actor.Kind != tenancy.PolicyUser || got.Actor.ID != principal.UserID.String() || got.Action != action || got.Resource.TenantID != before.TenantID || got.Resource.Kind != "task" || got.Resource.ID != before.ID.String() || !reflect.DeepEqual(got.Resource.Attributes, attrs) {
		t.Fatalf("trusted command facts = %+v; resource attributes %#v, want %#v", got, got.Resource.Attributes, attrs)
	}
}

func (f PolicyFixture) sameAnswer(got *contracts.Task, stored contracts.Task) bool {
	if got == nil {
		return false
	}
	if !f.Direct {
		// TenantID is deliberately absent from the HTTP representation. RLS and
		// policy-input assertions independently check its stored value.
		stored.TenantID = uuid.Nil
	}
	normalize := func(row contracts.Task) contracts.Task {
		row.CreatedAt, row.UpdatedAt = row.CreatedAt.UTC(), row.UpdatedAt.UTC()
		for _, at := range []**time.Time{&row.DeletedAt, &row.DueAt, &row.SLADeadline, &row.ResolvedAt} {
			if *at != nil {
				*at = new((*at).UTC())
			}
		}
		return row
	}
	return reflect.DeepEqual(normalize(*got), normalize(stored))
}
