// Package resolution runs durable task resolution over an explicit atomic store.
// It uses the shared domain decision and tenancy policy without importing a
// database, HTTP server, application constructor or broker.
package resolution

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/task/domain"
)

var ErrNotFound = errors.New("task: no such task in this tenant")

// Command carries trusted tenant and actor identities supplied by the host.
// Policy evaluates these identities against the locked task on every attempt.
type Command struct {
	Tenant     tenancy.Tenant
	Actor      tenancy.PolicyActor
	TaskID     uuid.UUID
	Resolution string
}

// Facts is the locked projection needed by resolution and its resource policy.
// A zero AssigneeID means unassigned. Returned timestamp pointers are owned by
// their recipient, never shared with storage.
type Facts struct {
	TenantID, TaskID             uuid.UUID
	Status, Priority, Resolution string
	AssigneeID                   uuid.UUID
	ResolvedAt                   *time.Time
}

// Result describes the resolved state. Service.Resolve returns it only after
// commit; StageAuthorized returns provisional facts in its caller's transaction.
type Result struct {
	TaskID     uuid.UUID
	Resolution string
	ResolvedAt *time.Time
	Changed    bool
}

type Change struct {
	Resolution string
	At         time.Time
}

// LockedTask holds one live task until its enclosing transaction finishes.
// StageResolution writes only resolution fields and one matching outbox event
// in that same transaction. It neither commits nor sends an external message.
type LockedTask interface {
	Facts() Facts
	StageResolution(context.Context, Change) error
}

// AtomicStore locks the current live tenant task, calls apply once, then commits.
// Callback errors roll back staged changes. Commit errors may have an uncertain
// outcome: callers must read or retry the same command to reconcile them.
// Implementations must not join an ambient transaction or retry only apply.
type AtomicStore interface {
	CommitLockedResolution(context.Context, tenancy.Tenant, uuid.UUID, tenancy.PolicyActor, func(LockedTask) error) error
}

type Service struct {
	store  AtomicStore
	policy tenancy.Policy
	now    func() time.Time
}

// New requires explicit storage, policy and time sources. Policy is mandatory
// for this entry point; legacy caller-authorized composition uses StageAuthorized.
func New(store AtomicStore, policy tenancy.Policy, now func() time.Time) (*Service, error) {
	if store == nil || policy == nil || now == nil {
		return nil, fmt.Errorf("task resolution: store, policy and clock are required")
	}
	return &Service{store: store, policy: policy, now: now}, nil
}

func (s *Service) Resolve(ctx context.Context, in Command) (Result, error) {
	if in.TaskID == uuid.Nil {
		return Result{}, ErrNotFound
	}
	if err := request(in, Facts{TenantID: in.Tenant.ID, TaskID: in.TaskID}).Validate(); err != nil {
		return Result{}, err
	}
	var result Result
	err := s.store.CommitLockedResolution(ctx, in.Tenant, in.TaskID, in.Actor, func(locked LockedTask) error {
		facts := locked.Facts()
		if err := target(in, facts); err != nil {
			return err
		}
		if _, err := tenancy.RequirePolicy(ctx, s.policy, request(in, facts)); err != nil {
			return err
		}
		var err error
		result, err = stage(ctx, locked, facts, in.Resolution, s.now)
		return err
	})
	if err != nil {
		return Result{}, err
	}
	return result, nil
}

// StageAuthorized is for trusted composition that has already checked current
// authority after locking this task. It performs no additional policy decision.
// Success is provisional: the caller still owns commit, rollback and any later
// product-level authority checks. It never opens or commits a transaction.
func StageAuthorized(ctx context.Context, locked LockedTask, in Command, now func() time.Time) (Result, error) {
	facts := locked.Facts()
	if err := target(in, facts); err != nil {
		return Result{}, err
	}
	return stage(ctx, locked, facts, in.Resolution, now)
}

func target(in Command, facts Facts) error {
	if in.TaskID == uuid.Nil || in.Tenant.ID == uuid.Nil || facts.TaskID != in.TaskID || facts.TenantID != in.Tenant.ID {
		return ErrNotFound
	}
	return nil
}

func request(in Command, facts Facts) tenancy.PolicyRequest {
	assignee := ""
	if facts.AssigneeID != uuid.Nil {
		assignee = facts.AssigneeID.String()
	}
	return tenancy.PolicyRequest{Tenant: in.Tenant, Actor: in.Actor, Action: "task:resolve",
		Resource: tenancy.PolicyResource{TenantID: facts.TenantID, Kind: "task", ID: facts.TaskID.String(),
			Attributes: map[string]any{"status": facts.Status, "priority": facts.Priority, "assignee_id": assignee}}}
}

func stage(ctx context.Context, locked LockedTask, facts Facts, text string, now func() time.Time) (Result, error) {
	decision, err := domain.Resolve(facts.Status, facts.Resolution, text)
	if err != nil {
		return Result{}, err
	}
	result := Result{TaskID: facts.TaskID, Resolution: facts.Resolution}
	if facts.ResolvedAt != nil {
		result.ResolvedAt = new(*facts.ResolvedAt)
	}
	if !decision.Changed {
		return result, nil
	}
	// Retain the existing Task timestamp precision across storage round trips.
	at := now().UTC().Truncate(time.Microsecond)
	if err := locked.StageResolution(ctx, Change{Resolution: decision.Text, At: at}); err != nil {
		return Result{}, err
	}
	return Result{TaskID: facts.TaskID, Resolution: decision.Text, ResolvedAt: new(at), Changed: true}, nil
}
