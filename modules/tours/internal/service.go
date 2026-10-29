package internal

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/tours/contracts"
)

type Service struct {
	Clock  contracts.Clock
	Policy tenancy.Policy
	App    appname.Name
}

var _ contracts.Service = (*Service)(nil)

func NewService(clock contracts.Clock, policy tenancy.Policy, app appname.Name) *Service {
	if clock == nil || policy == nil {
		panic("tours: clock and policy are required")
	}
	return &Service{Clock: clock, Policy: policy, App: app}
}

func (s *Service) Start(ctx context.Context, tx db.Tx[db.Tenant], in contracts.Start) (*contracts.Run, error) {
	person, err := s.actor(ctx, tx, in.TourID, "run")
	if err != nil {
		return nil, err
	}
	if in.Source != "manual" && in.Source != "automatic" || in.TourID == "" || in.Edition == 0 || in.ContractHash == "" {
		return nil, crud.ErrInvalid
	}
	at := atTime(s.Clock)
	if in.Source == "automatic" {
		if err := lockOffer(ctx, tx, s.App, person, at); err != nil {
			return nil, err
		}
	}
	steps := slices.Clone(in.StepIDs)
	if len(steps) == 0 {
		return nil, crud.ErrInvalid
	}
	run := &contracts.Run{
		Base: entity.Base{}, App: s.App.String(), PersonID: person,
		TourID: in.TourID, Edition: in.Edition,
		ContentRevision: in.ContractHash, ContractHash: in.ContractHash,
		StepIDs: steps, Source: in.Source, Status: "reserved", CurrentStep: steps[0],
		Done: []string{}, Skipped: []string{}, Revision: 1, StartedAt: at,
	}
	if err := crud.Create(ctx, tx, run); err != nil {
		return nil, err
	}
	if in.Source == "automatic" {
		if err := reserveOffer(ctx, tx, run, at); err != nil {
			return nil, err
		}
	}
	return run, nil
}

func atTime(clock contracts.Clock) time.Time { return clock().UTC() }

func (s *Service) Get(ctx context.Context, tx db.Tx[db.Tenant], person, id uuid.UUID) (*contracts.Run, error) {
	actor, err := s.actor(ctx, tx, "", "read")
	if err != nil {
		return nil, err
	}
	if actor != person {
		return nil, crud.ErrNotFound
	}
	run, err := crud.Get[*contracts.Run](tx, id)
	if errors.Is(err, crud.ErrNotFound) || run != nil && run.PersonID != actor {
		return nil, crud.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if _, err := s.actor(ctx, tx, string(run.TourID), "run"); err != nil {
		return nil, err
	}
	return run, nil
}

func (s *Service) Resumable(ctx context.Context, tx db.Tx[db.Tenant]) ([]*contracts.Run, error) {
	person, err := s.actor(ctx, tx, "", "read")
	if err != nil {
		return nil, err
	}
	var rows []*contracts.Run
	err = tx.DB().WithContext(ctx).
		Where("app = ? AND person_id = ? AND status IN ?", s.App.String(), person, []string{"reserved", "active", "paused", "later"}).
		Order("updated_at DESC, id").Limit(50).Find(&rows).Error
	if err != nil {
		return nil, crud.Classify(err)
	}
	return rows, nil
}

func (s *Service) Act(ctx context.Context, tx db.Tx[db.Tenant], person, id uuid.UUID, action contracts.Action) (*contracts.Run, error) {
	actor, err := s.actor(ctx, tx, "", "run")
	if err != nil {
		return nil, err
	}
	if actor != person {
		return nil, crud.ErrNotFound
	}
	run, err := crud.GetForUpdate[*contracts.Run](tx, id)
	if err != nil || run.PersonID != actor {
		return nil, crud.ErrNotFound
	}
	if _, err := s.actor(ctx, tx, string(run.TourID), "run"); err != nil {
		return nil, err
	}
	before := run.Status
	action.At = s.Clock().UTC()
	if err := transition(run, action, run.StepIDs); err != nil {
		return nil, err
	}
	if err := crud.Update(ctx, tx, run, "status", "current_step", "done", "skipped", "revision", "later_until", "viewed_at", "completed_at", "dismissed_at", "updated_at"); err != nil {
		return nil, err
	}
	if err := s.publish(ctx, tx, run, action, before); err != nil {
		return nil, err
	}
	return run, nil
}

func (s *Service) actor(ctx context.Context, tx db.Tx[db.Tenant], id, action string) (uuid.UUID, error) {
	person, ok := tenancy.ActorFrom(ctx)
	if !ok {
		return uuid.Nil, crud.ErrInvalid
	}
	decision, err := tenancy.RequirePolicy(ctx, s.Policy, tenancy.PolicyRequest{
		Tenant: db.TenantOf(tx), Actor: tenancy.PolicyActor{Kind: tenancy.PolicyUser, ID: person.String()},
		Action:   "tours:" + action,
		Resource: tenancy.PolicyResource{TenantID: db.TenantOf(tx).ID, Kind: "tour", ID: id},
	})
	if err != nil || !decision.Allowed {
		return uuid.Nil, err
	}
	return person, nil
}

func (s *Service) publish(ctx context.Context, tx db.Tx[db.Tenant], run *contracts.Run, action contracts.Action, before string) error {
	payload := contracts.Event{RunID: run.ID, TourID: run.TourID, Edition: run.Edition, StepID: action.Step, Source: run.Source, PersonID: run.PersonID, At: action.At}
	if action.Kind == "viewed" && before == "reserved" {
		if err := events.Publish(ctx, tx, contracts.EventStarted, payload); err != nil {
			return err
		}
	}
	if action.Kind == "viewed" {
		if err := events.Publish(ctx, tx, contracts.EventStepViewed, payload); err != nil {
			return err
		}
	}
	if run.Status == "completed" && before != "completed" {
		if err := events.Publish(ctx, tx, contracts.EventCompleted, payload); err != nil {
			return err
		}
	}
	if run.Status == "dismissed" && before != "dismissed" {
		return events.Publish(ctx, tx, contracts.EventDismissed, payload)
	}
	return nil
}

func lockOffer(ctx context.Context, tx db.Tx[db.Tenant], app appname.Name, person uuid.UUID, at time.Time) error {
	name := fmt.Sprintf("tours.offer/%s/%s/%s", app, db.TenantOf(tx).ID, person)
	dbtx := tx.DB().WithContext(ctx)
	if err := dbtx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", name).Error; err != nil {
		return err
	}
	tenantID := db.TenantOf(tx).ID
	if err := dbtx.Exec(`INSERT INTO tours_person_frequency (tenant_id, app, person_id)
		VALUES (?, ?, ?) ON CONFLICT (tenant_id, app, person_id) DO NOTHING`, tenantID, app.String(), person).Error; err != nil {
		return err
	}
	if err := dbtx.Exec(`UPDATE tours_reservations SET released_at = ?, revision = revision + 1
		WHERE tenant_id = ? AND app = ? AND person_id = ? AND consumed_at IS NULL
		AND released_at IS NULL AND expires_at <= ?`, at, tenantID, app.String(), person, at).Error; err != nil {
		return err
	}
	return nil
}

func reserveOffer(ctx context.Context, tx db.Tx[db.Tenant], run *contracts.Run, at time.Time) error {
	tenantID := db.TenantOf(tx).ID
	result := tx.DB().WithContext(ctx).Exec(`INSERT INTO tours_reservations
		(tenant_id, app, person_id, tour_id, edition, run_id, reserved_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (tenant_id, app, person_id)
		WHERE consumed_at IS NULL AND released_at IS NULL DO NOTHING`,
		tenantID, run.App, run.PersonID, run.TourID, run.Edition, run.ID, at, at.Add(2*time.Minute))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return crud.ErrConflict
	}
	return nil
}
