package rest_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/idempotency"
	"github.com/septagon-oss/platformkit/kit/porttest"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

var guidedID = uuid.MustParse("30000000-0000-4000-8000-000000000001")
var guidedAt = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

func guidedFixture() entity.GuidedSession {
	steps := make([]entity.GuidedStep, 3)
	for i, name := range []string{"Prepare", "Repeat", "Review"} {
		steps[i] = entity.GuidedStep{ID: uuid.MustParse(fmt.Sprintf("40000000-0000-4000-8000-%012d", i+1)), Title: name, Items: []entity.GuidedItem{}}
	}
	for i, step := range []int{0, 1, 1, 2} {
		steps[step].Sets = append(steps[step].Sets, uuid.MustParse(fmt.Sprintf("50000000-0000-4000-8000-%012d", i+1)))
	}
	steps[0].Text = "Place the two strips beside the frame."
	steps[0].Items = []entity.GuidedItem{{Label: "Strips", Quantity: "2"}}
	steps[0].Timer = &entity.GuidedTimer{Name: "Wait", Seconds: 600}
	steps[1].Text = "Follow the instruction, then confirm each set."
	steps[1].Target = &entity.GuidedTarget{Kind: "repetitions", Value: 12}
	steps[1].RestBetweenSeconds, steps[1].RestAfterSeconds = 45, 20
	steps[2].Text = "Review the result."
	steps[2].Target = &entity.GuidedTarget{Kind: "seconds", Value: 40}
	steps[2].RestAfterSeconds = 10
	return entity.GuidedSession{ID: guidedID, Title: "Practice sequence", Steps: steps, Progress: &entity.GuidedProgress{Revision: 1, Completed: []entity.GuidedCompletion{}}}
}

type guidedState struct {
	ID       uuid.UUID
	Actor    uuid.UUID
	Active   bool
	Session  entity.GuidedSession
	Complete map[uuid.UUID]entity.CompleteGuidedSet
	Finish   *entity.FinishGuidedSession
}

func initialGuided() guidedState {
	return guidedState{ID: guidedID, Actor: principal, Active: true, Session: guidedFixture(), Complete: map[uuid.UUID]entity.CompleteGuidedSet{}}
}

// The fixture's SQL and memory adapters share this decision. Persistence and
// transport are outside it; returning an error leaves the input untouched.
func decideGuided(state guidedState, actor uuid.UUID, in any, at time.Time) (guidedState, any, string, error) {
	refuse := func(reason string) (guidedState, any, string, error) {
		return guidedState{}, nil, "", problem.Conflict(reason)
	}
	if !state.Active || state.Actor != actor {
		return guidedState{}, nil, "", crud.ErrNotFound
	}
	data, _ := json.Marshal(state)
	var next guidedState
	_ = json.Unmarshal(data, &next)
	s, p := &next.Session, next.Session.Progress
	switch input := in.(type) {
	case entity.CompleteGuidedSet:
		position, found := 0, false
		for _, step := range s.Steps {
			for _, set := range step.Sets {
				if step.ID == input.StepID && set == input.SetID {
					found = true
					break
				}
				position++
			}
			if found {
				break
			}
		}
		if !found {
			return guidedState{}, nil, "", crud.ErrNotFound
		}
		if old, ok := next.Complete[input.SetID]; ok {
			if old != input {
				return refuse("identity conflict")
			}
			return state, p.Completed[position], "", nil
		}
		if p.Finished != nil {
			return refuse("finished")
		}
		if input.IfRevision != p.Revision {
			return refuse("stale revision")
		}
		if position != len(p.Completed) {
			return refuse("out of order")
		}
		receipt := entity.GuidedCompletion{SessionID: s.ID, StepID: input.StepID, SetID: input.SetID, Revision: p.Revision + 1, CompletedAt: at}
		p.Completed = append(p.Completed, receipt)
		p.Revision++
		next.Complete[input.SetID] = input
		return next, receipt, "practice.set.completed", nil
	case entity.FinishGuidedSession:
		if next.Finish != nil {
			if *next.Finish != input {
				return refuse("identity conflict")
			}
			return state, *p.Finished, "", nil
		}
		if input.IfRevision != p.Revision {
			return refuse("stale revision")
		}
		total := 0
		for _, step := range s.Steps {
			total += len(step.Sets)
		}
		summary := entity.GuidedSummary{SessionID: s.ID, Revision: p.Revision + 1, FinishedAt: at, CompletedSets: int32(len(p.Completed)), TotalSets: int32(total), Title: "Session ended", Text: "Recorded sets are listed in your history.", Facts: []entity.GuidedFact{}}
		p.Revision++
		p.Finished = &summary
		next.Finish = &input
		return next, summary, "practice.session.finished", nil
	}
	return refuse("unknown command")
}

type guidedRow struct {
	crud.Base
	Data   string
	Actor  uuid.UUID
	Active bool
}

func (guidedRow) TableName() string { return "guided_runs" }

const guidedDDL = `CREATE TABLE guided_runs (id uuid PRIMARY KEY, tenant_id uuid NOT NULL, created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(), deleted_at timestamptz, data text NOT NULL, actor uuid NOT NULL, active boolean NOT NULL);
ALTER TABLE guided_runs ENABLE ROW LEVEL SECURITY; ALTER TABLE guided_runs FORCE ROW LEVEL SECURITY;
CREATE POLICY guided_tenant ON guided_runs USING(platformkit_tenant_match(tenant_id)) WITH CHECK(platformkit_tenant_match(tenant_id));`

type guidedSQL struct{ clock *porttest.Clock }

func (s *guidedSQL) state(tx db.Tx[db.Tenant], actor, id uuid.UUID, lock bool) (guidedState, error) {
	var row *guidedRow
	var err error
	if lock {
		row, err = crud.GetForUpdate[*guidedRow](tx, id)
	} else {
		row, err = crud.Get[*guidedRow](tx, id)
	}
	if err != nil {
		return guidedState{}, err
	}
	if !row.Active || row.Actor != actor {
		return guidedState{}, crud.ErrNotFound
	}
	var out guidedState
	err = json.Unmarshal([]byte(row.Data), &out)
	out.Active, out.Actor = row.Active, row.Actor
	return out, err
}
func (s *guidedSQL) load(_ context.Context, tx db.Tx[db.Tenant], actor, id uuid.UUID) (entity.GuidedSession, error) {
	state, err := s.state(tx, actor, id, false)
	if err != nil {
		return entity.GuidedSession{}, err
	}
	return state.Session, nil
}
func (s *guidedSQL) run(ctx context.Context, tx db.Tx[db.Tenant], actor, id uuid.UUID, in any) (any, error) {
	state, err := s.state(tx, actor, id, true)
	if err != nil {
		return nil, err
	}
	next, result, event, err := decideGuided(state, actor, in, s.clock.Now())
	if err != nil || event == "" {
		return result, err
	}
	data, err := json.Marshal(next)
	if err != nil {
		return nil, err
	}
	if err = tx.DB().Model(&guidedRow{}).Where("id = ?", id).Update("data", string(data)).Error; err != nil {
		return nil, err
	}
	if err = events.Publish(ctx, tx, event, result); err != nil {
		return nil, err
	}
	return result, nil
}
func (s *guidedSQL) recheck(_ context.Context, tx db.Tx[db.Tenant], actor, id uuid.UUID, in any) (bool, error) {
	state, err := s.state(tx, actor, id, true)
	if errors.Is(err, crud.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	switch input := in.(type) {
	case entity.CompleteGuidedSet:
		if previous, ok := state.Complete[input.SetID]; ok {
			return previous == input, nil
		}
	case entity.FinishGuidedSession:
		if state.Finish != nil {
			return *state.Finish == input, nil
		}
	}
	return true, nil
}
func (s *guidedSQL) spec() rest.SessionSpec {
	return rest.SessionSpec{Module: "practice", Entity: "session", Path: "/sessions", Read: "practice:read", Operations: []httpx.CRUD{rest.Read}, Present: entity.EntryHints{Session: true}, Session: &rest.SessionRead{Load: s.load,
		Complete: &rest.SessionCommand[entity.CompleteGuidedSet, entity.GuidedCompletion]{Options: rest.CommandOptions{Auth: httpx.Permission("practice:complete")}, Events: []string{"practice.set.completed"},
			Run: func(ctx context.Context, tx db.Tx[db.Tenant], actor, id uuid.UUID, in entity.CompleteGuidedSet) (entity.GuidedCompletion, error) {
				out, err := s.run(ctx, tx, actor, id, in)
				if err != nil {
					return entity.GuidedCompletion{}, err
				}
				return out.(entity.GuidedCompletion), nil
			},
			Recheck: func(ctx context.Context, tx db.Tx[db.Tenant], actor, id uuid.UUID, in entity.CompleteGuidedSet) (bool, error) {
				return s.recheck(ctx, tx, actor, id, in)
			}},
		Finish: &rest.SessionCommand[entity.FinishGuidedSession, entity.GuidedSummary]{Options: rest.CommandOptions{Auth: httpx.Permission("practice:finish")}, Events: []string{"practice.session.finished"},
			Run: func(ctx context.Context, tx db.Tx[db.Tenant], actor, id uuid.UUID, in entity.FinishGuidedSession) (entity.GuidedSummary, error) {
				out, err := s.run(ctx, tx, actor, id, in)
				if err != nil {
					return entity.GuidedSummary{}, err
				}
				return out.(entity.GuidedSummary), nil
			},
			Recheck: func(ctx context.Context, tx db.Tx[db.Tenant], actor, id uuid.UUID, in entity.FinishGuidedSession) (bool, error) {
				return s.recheck(ctx, tx, actor, id, in)
			}},
	}}
}

type guidedHarness struct {
	api     *httpx.API
	router  chi.Router
	admin   *sql.DB
	conn    *db.Conn
	service *guidedSQL
}

func newGuided(t *testing.T, change ...func(*rest.SessionSpec)) guidedHarness {
	t.Helper()
	admin, conn := dbtest.Schema(t)
	if _, err := admin.ExecContext(t.Context(), guidedDDL); err != nil {
		t.Fatal(err)
	}
	state := initialGuided()
	data, _ := json.Marshal(state)
	if _, err := admin.ExecContext(t.Context(), "INSERT INTO guided_runs(id,tenant_id,data,actor,active) VALUES($1,$2,$3,$4,true)", guidedID, acme.ID, string(data), principal); err != nil {
		t.Fatal(err)
	}
	service := &guidedSQL{clock: porttest.NewClock(guidedAt)}
	api, router := httpx.New(httpx.Options{Cache: cache.Memory("guided"), Idempotency: idempotency.Memory(nil), PublicHost: host, Tenants: caller{}, Conn: conn, Authorize: caller{}, Log: slog.New(slog.DiscardHandler), Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
		return tenancy.Principal{UserID: principal}, true, nil
	}})
	spec := service.spec()
	for _, edit := range change {
		edit(&spec)
	}
	spec.Mount(api.Surfaces("practice"))
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatal(err)
	}
	return guidedHarness{api, router, admin, conn, service}
}
func (h guidedHarness) run(t *testing.T, tenant tenancy.Tenant, in any) (any, error) {
	t.Helper()
	var result any
	ctx := tenancy.WithTenant(t.Context(), tenant)
	err := db.Run(ctx, h.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		result, err = h.service.run(ctx, tx, principal, guidedID, in)
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
func (h guidedHarness) read(t *testing.T) entity.GuidedSession {
	t.Helper()
	var result entity.GuidedSession
	err := db.Run(tenancy.WithTenant(t.Context(), acme), h.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		result, err = h.service.load(ctx, tx, principal, guidedID)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func (h guidedHarness) events(t *testing.T) int {
	t.Helper()
	var count int
	if err := h.admin.QueryRowContext(t.Context(), "SELECT count(*) FROM platformkit_outbox WHERE name LIKE 'practice.%'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}
func guidedInput(position int, revision int64) entity.CompleteGuidedSet {
	s := guidedFixture()
	step, set, _ := s.Pair(position)
	return entity.CompleteGuidedSet{StepID: s.Steps[step].ID, SetID: s.Steps[step].Sets[set], IfRevision: revision}
}
func guidedCall(h http.Handler, method, path, body, kind, key string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://"+host+path, strings.NewReader(body))
	r.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
	r.Header.Set("Content-Type", kind)
	if key != "" {
		r.Header.Set("Idempotency-Key", key)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

type guidedFake struct {
	*porttest.Fake
	mu    sync.Mutex
	store *porttest.Store[guidedState]
}

func newGuidedFake(ctx context.Context) *guidedFake {
	f := &guidedFake{Fake: porttest.NewFake(guidedAt, []string{"practice.set.completed", "practice.session.finished"}), store: porttest.NewStore(func(s guidedState) uuid.UUID { return s.ID })}
	f.store.Put(ctx, initialGuided())
	return f
}
func (f *guidedFake) run(ctx context.Context, in any) (any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	state, err := f.store.Get(ctx, guidedID)
	if err != nil {
		return nil, err
	}
	next, out, event, err := decideGuided(state, principal, in, f.Clock.Now())
	if err != nil {
		return nil, err
	}
	if event != "" {
		if err = f.Events.Emit(event); err != nil {
			return nil, err
		}
		f.store.Put(ctx, next)
	}
	return out, nil
}
