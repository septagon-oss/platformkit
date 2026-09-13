package resolution_test

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/task/domain"
	"github.com/septagon-oss/platformkit/modules/task/resolution"
)

type policyFunc func(context.Context, tenancy.PolicyRequest) (tenancy.PolicyDecision, error)

func (f policyFunc) Decide(ctx context.Context, r tenancy.PolicyRequest) (tenancy.PolicyDecision, error) {
	return f(ctx, r)
}

// This store applies supplied changes without deciding when they are allowed.
// It gives the real service atomic staging and controllable commit outcomes.
type memoryStore struct {
	mu                    sync.Mutex
	facts                 resolution.Facts
	events                []resolution.Change
	writeErr, commitErr   error
	committedDespiteError bool
	order                 []string
}

type memoryLocked struct {
	store  *memoryStore
	facts  resolution.Facts
	events []resolution.Change
}

func clone(f resolution.Facts) resolution.Facts {
	if f.ResolvedAt != nil {
		f.ResolvedAt = new(*f.ResolvedAt)
	}
	return f
}

func (s *memoryStore) CommitLockedResolution(ctx context.Context, tenant tenancy.Tenant, id uuid.UUID, _ tenancy.PolicyActor, apply func(resolution.LockedTask) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.facts.TaskID != id || s.facts.TenantID != tenant.ID {
		return resolution.ErrNotFound
	}
	s.order = append(s.order, "lock")
	locked := &memoryLocked{store: s, facts: clone(s.facts)}
	if err := apply(locked); err != nil {
		return err
	}
	if s.commitErr == nil || s.committedDespiteError {
		s.facts = clone(locked.facts)
		s.events = append(s.events, locked.events...)
		s.order = append(s.order, "commit")
	}
	return s.commitErr
}

func (l *memoryLocked) Facts() resolution.Facts { return clone(l.facts) }
func (l *memoryLocked) StageResolution(_ context.Context, c resolution.Change) error {
	l.store.order = append(l.store.order, "write")
	l.facts.Status, l.facts.Resolution, l.facts.ResolvedAt = domain.StatusResolved, c.Resolution, new(c.At)
	l.events = append(l.events, c)
	return l.store.writeErr
}

func fixture() (*memoryStore, resolution.Command, time.Time) {
	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "one"}
	id := uuid.New()
	return &memoryStore{facts: resolution.Facts{TenantID: tenant.ID, TaskID: id, Status: domain.StatusOpen, Priority: "high", AssigneeID: uuid.New()}},
		resolution.Command{Tenant: tenant, TaskID: id, Actor: tenancy.PolicyActor{Kind: tenancy.PolicyUser, ID: uuid.NewString()}, Resolution: "  repaired  "},
		time.Date(2026, 9, 14, 10, 11, 12, 345678999, time.FixedZone("offset", 3600))
}

func allow(context.Context, tenancy.PolicyRequest) (tenancy.PolicyDecision, error) {
	return tenancy.PolicyDecision{Allowed: true}, nil
}

func service(t *testing.T, store resolution.AtomicStore, policy tenancy.Policy, now func() time.Time) *resolution.Service {
	t.Helper()
	s, err := resolution.New(store, policy, now)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestResolveUsesLockedPolicyFactsAndCommitsOneChange(t *testing.T) {
	store, command, at := fixture()
	policy := policyFunc(func(_ context.Context, req tenancy.PolicyRequest) (tenancy.PolicyDecision, error) {
		store.order = append(store.order, "policy")
		if req.Tenant != command.Tenant || req.Actor != command.Actor || req.Action != "task:resolve" || req.Resource.TenantID != command.Tenant.ID || req.Resource.ID != command.TaskID.String() ||
			req.Resource.Attributes["status"] != "open" || req.Resource.Attributes["priority"] != "high" || req.Resource.Attributes["assignee_id"] != store.facts.AssigneeID.String() {
			t.Errorf("policy did not receive trusted current facts: %+v", req)
		}
		return tenancy.PolicyDecision{Allowed: true}, nil
	})
	svc := service(t, store, policy, func() time.Time { store.order = append(store.order, "clock"); return at })
	got, err := svc.Resolve(t.Context(), command)
	wantAt := at.UTC().Truncate(time.Microsecond)
	if err != nil || !got.Changed || got.Resolution != "repaired" || got.ResolvedAt == nil || !got.ResolvedAt.Equal(wantAt) || len(store.events) != 1 {
		t.Fatalf("Resolve = %+v, %v; events=%v", got, err, store.events)
	}
	if !slices.Equal(store.order, []string{"lock", "policy", "clock", "write", "commit"}) {
		t.Fatalf("effects occurred in order %v", store.order)
	}
	*got.ResolvedAt = time.Time{}
	if !store.facts.ResolvedAt.Equal(wantAt) || !store.events[0].At.Equal(wantAt) {
		t.Fatal("returned timestamp aliases committed state")
	}
}

func TestRetriesAndRefusalsDoNotWriteOrSampleTime(t *testing.T) {
	for _, tc := range []struct {
		name, status, requested string
		want                    error
	}{
		{"same", "resolved", " repaired ", nil},
		{"empty", "resolved", "", nil},
		{"different", "resolved", "inspected", domain.ErrDifferentResolution},
		{"closed", "closed", "repaired", domain.ErrClosed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, command, at := fixture()
			store.facts.Status, store.facts.Resolution, store.facts.ResolvedAt = tc.status, " repaired ", new(at)
			command.Resolution = tc.requested
			calls := 0
			policy := policyFunc(func(context.Context, tenancy.PolicyRequest) (tenancy.PolicyDecision, error) {
				calls++
				return tenancy.PolicyDecision{Allowed: true}, nil
			})
			svc := service(t, store, policy, func() time.Time { t.Fatal("a retry/refusal sampled time"); return time.Time{} })
			got, err := svc.Resolve(t.Context(), command)
			if !errors.Is(err, tc.want) || got.Changed || calls != 1 || len(store.events) != 0 || store.facts.Resolution != " repaired " || !store.facts.ResolvedAt.Equal(at) {
				t.Fatalf("result=%+v error=%v calls=%d facts=%+v", got, err, calls, store.facts)
			}
			if err == nil {
				if got.Resolution != " repaired " || got.ResolvedAt == nil || !got.ResolvedAt.Equal(at) {
					t.Fatalf("retry changed recorded facts: %+v", got)
				}
				*got.ResolvedAt = time.Time{}
				if !store.facts.ResolvedAt.Equal(at) {
					t.Fatal("retry timestamp aliases storage")
				}
			} else if !reflect.DeepEqual(got, resolution.Result{}) {
				t.Fatal("refusal returned a success result")
			}
		})
	}
}

func TestPolicyRefusalPrecedesLifecycleResults(t *testing.T) {
	for _, status := range []string{"open", "resolved", "closed"} {
		for _, unavailable := range []bool{false, true} {
			store, command, at := fixture()
			store.facts.Status = status
			policy := policyFunc(func(context.Context, tenancy.PolicyRequest) (tenancy.PolicyDecision, error) {
				if unavailable {
					return tenancy.PolicyDecision{}, errors.New("provider down")
				}
				return tenancy.PolicyDecision{}, nil
			})
			svc := service(t, store, policy, func() time.Time { t.Fatal("refused policy sampled time"); return at })
			got, err := svc.Resolve(t.Context(), command)
			want := tenancy.ErrPolicyDenied
			if unavailable {
				want = tenancy.ErrPolicyUnavailable
			}
			if !errors.Is(err, want) || !reflect.DeepEqual(got, resolution.Result{}) || len(store.events) != 0 || store.facts.Status != status {
				t.Fatalf("status=%s unavailable=%v: result=%+v error=%v", status, unavailable, got, err)
			}
		}
	}
}

func TestWriteAndCommitFailuresReturnNoSuccessResult(t *testing.T) {
	for _, mode := range []string{"write", "rollback", "commit acknowledged as failure"} {
		t.Run(mode, func(t *testing.T) {
			store, command, at := fixture()
			failure := errors.New("storage outcome")
			if mode == "write" {
				store.writeErr = failure
			} else {
				store.commitErr = failure
			}
			store.committedDespiteError = mode == "commit acknowledged as failure"
			svc := service(t, store, policyFunc(allow), func() time.Time { return at })
			got, err := svc.Resolve(t.Context(), command)
			if !errors.Is(err, failure) || !reflect.DeepEqual(got, resolution.Result{}) {
				t.Fatalf("failure = %+v, %v", got, err)
			}
			committed := store.committedDespiteError
			if (store.facts.ResolvedAt != nil) != committed || (len(store.events) == 1) != committed {
				t.Fatal("observed state disagrees with commit outcome")
			}
			store.writeErr, store.commitErr = nil, nil
			again, err := svc.Resolve(t.Context(), command)
			if err != nil || again.Changed == committed || len(store.events) != 1 || again.ResolvedAt == nil || !again.ResolvedAt.Equal(at.UTC().Truncate(time.Microsecond)) {
				t.Fatalf("retry failed to reconcile observed state: %+v, %v", again, err)
			}
		})
	}
}

func TestAbsentOrForeignTaskNeverReachesPolicy(t *testing.T) {
	for _, foreign := range []bool{false, true} {
		store, command, at := fixture()
		if foreign {
			command.Tenant.ID = uuid.New()
		} else {
			command.TaskID = uuid.New()
		}
		svc := service(t, store, policyFunc(func(context.Context, tenancy.PolicyRequest) (tenancy.PolicyDecision, error) {
			t.Fatal("missing task reached policy")
			return tenancy.PolicyDecision{}, nil
		}), func() time.Time { return at })
		if _, err := svc.Resolve(t.Context(), command); !errors.Is(err, resolution.ErrNotFound) {
			t.Fatal(err)
		}
	}
}

func TestConcurrentResolutionUsesOneCommittedDecision(t *testing.T) {
	store, command, at := fixture()
	svc := service(t, store, policyFunc(allow), func() time.Time { return at })
	var workers sync.WaitGroup
	for range 16 {
		workers.Go(func() {
			if _, err := svc.Resolve(t.Context(), command); err != nil {
				t.Error(err)
			}
		})
	}
	workers.Wait()
	if len(store.events) != 1 || store.facts.Resolution != "repaired" {
		t.Fatal("contending commands committed duplicate changes")
	}
}

func TestConstructionRequiresEveryDependency(t *testing.T) {
	store, _, at := fixture()
	clock := func() time.Time { return at }
	for _, deps := range []struct {
		store  resolution.AtomicStore
		policy tenancy.Policy
		now    func() time.Time
	}{
		{nil, policyFunc(allow), clock}, {store, nil, clock}, {store, policyFunc(allow), nil},
	} {
		if _, err := resolution.New(deps.store, deps.policy, deps.now); err == nil {
			t.Fatal("missing dependency accepted")
		}
	}
}
