package rest

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// SessionSpec declares a guided read without a storage entity. Mount requires
// exactly Read, Present.Session and an authenticated app guard; no CRUD writes.
type SessionSpec = Spec[entity.Entity]

// SessionRead binds module-owned state to the session presentation. UUID arguments
// are actor then session. Load and commands use the caller's tenant transaction.
type SessionRead struct {
	Load     func(context.Context, db.Tx[db.Tenant], uuid.UUID, uuid.UUID) (entity.GuidedSession, error)
	Complete *SessionCommand[entity.CompleteGuidedSet, entity.GuidedCompletion]
	Finish   *SessionCommand[entity.FinishGuidedSession, entity.GuidedSummary]
}

// SessionCommand owns authority, locking, expected revisions and natural-key
// receipts. Run commits state and declared events together; Recheck only checks
// current authority and retained intent when recovering a cached HTTP answer.
type SessionCommand[I, O any] struct {
	Run     func(context.Context, db.Tx[db.Tenant], uuid.UUID, uuid.UUID, I) (O, error)
	Recheck func(context.Context, db.Tx[db.Tenant], uuid.UUID, uuid.UUID, I) (bool, error)
	Options CommandOptions
	Events  []string
}

func (s Spec[T]) checkSession() {
	fail := func(why string) { panic("rest: session " + s.Module + "." + s.Entity + ": " + why) }
	if reflect.TypeFor[T]() != reflect.TypeFor[entity.Entity]() {
		fail("use SessionSpec")
	}
	if s.Session.Load == nil || !s.Present.Session {
		fail("Load and Present.Session are required")
	}
	if !slices.Equal(s.Operations, []httpx.CRUD{Read}) {
		fail("Operations must be [read]")
	}
	if !sessionAuth(s.readAuth()) {
		fail("Read requires an authenticated app guard")
	}
	probe := s
	probe.Write = "session:unused"
	if bad := probe.identityFault(); bad != "" {
		fail(bad)
	}
	if bad := s.operationsFault(); bad != "" {
		fail(bad)
	}
	v := reflect.ValueOf(s)
	for i := range v.NumField() {
		name := v.Type().Field(i).Name
		switch name {
		case "Module", "Entity", "Path", "Read", "ReadAuth", "Operations", "Session", "Present":
		default:
			if !v.Field(i).IsZero() {
				fail(name + " is incompatible with Session")
			}
		}
	}
	hints := s.Present
	hints.Session = false
	if !reflect.ValueOf(hints).IsZero() {
		fail("Session has no storage presentation hints")
	}
	checkSessionCommand(s.Session.Complete, fail)
	checkSessionCommand(s.Session.Finish, fail)
}

func sessionAuth(auth httpx.Auth) bool {
	return !auth.Operator() && (auth.NamesAGrant() || auth == httpx.SignedIn() || (auth.Feature() != "" && auth == httpx.SignedIn().Needing(auth.Feature())))
}

func checkSessionCommand[I, O any](c *SessionCommand[I, O], fail func(string)) {
	if c == nil {
		return
	}
	if c.Run == nil || c.Recheck == nil || !sessionAuth(c.Options.Auth) || c.Options.Auth.HasReplayCheck() {
		fail("command needs Run, Recheck and one authenticated Auth")
	}
	opts := c.Options
	opts.Auth = httpx.Auth{}
	if !reflect.ValueOf(opts).IsZero() {
		fail("command supports only Options.Auth")
	}
	if len(c.Events) == 0 {
		fail("command must declare domain events")
	}
	seen := map[string]bool{}
	for _, name := range c.Events {
		if name == "" || seen[name] {
			fail("command event names must be distinct and nonempty")
		}
		seen[name] = true
	}
}

func (s Spec[T]) mountSession(surfaces httpx.Surfaces) {
	s.checkSession()
	load := func(ctx context.Context, tx db.Tx[db.Tenant], actor, id uuid.UUID) (entity.GuidedSession, error) {
		out, err := s.Session.Load(ctx, tx, actor, id)
		if err != nil {
			return entity.GuidedSession{}, err
		}
		if out.ID != id {
			return entity.GuidedSession{}, fmt.Errorf("session: read identity mismatch")
		}
		if err := out.Validate(s.Session.Complete != nil || s.Session.Finish != nil); err != nil {
			return entity.GuidedSession{}, err
		}
		return out, nil
	}
	res := httpx.Resource{Module: s.Module, Entity: s.Entity, Path: s.Path, Schema: s.Schema(), Present: s.Present,
		ReadBy: s.readAuth(), Operations: []httpx.CRUD{Read}, Screen: surfaces.App.PagePath(s.Path),
		GuidedCommands: [2]bool{s.Session.Complete != nil, s.Session.Finish != nil}}
	res.Schema.Path = surfaces.App.Prefix() + s.Path
	res.Guided = func(ctx context.Context, id uuid.UUID) (entity.GuidedSession, error) {
		tx, err := transaction(ctx)
		if err != nil {
			return entity.GuidedSession{}, err
		}
		actor, _ := tenancy.PrincipalFrom(ctx)
		return load(ctx, tx, actor.UserID, id)
	}
	res.Get = func(ctx context.Context, id uuid.UUID) (map[string]any, error) {
		out, err := res.Guided(ctx, id)
		if err != nil {
			return nil, err
		}
		return row(out)
	}
	s.restrictResource(&res)
	surfaces.RegisterResource(res)
	Operation(surfaces.App, s.op("read", http.MethodGet, s.item(), 0, "Read session", ""), s.readAuth(),
		func(ctx context.Context, tx db.Tx[db.Tenant], actor uuid.UUID, in *idInput) (entity.GuidedSession, error) {
			return load(ctx, tx, actor, in.ID)
		}, OperationOptions{CacheControl: "private, no-store"})
	mountSessionCommand(surfaces, s, "complete-set", s.Session.Complete, load)
	mountSessionCommand(surfaces, s, "finish", s.Session.Finish, load)
}

func mountSessionCommand[I, O any, T entity.Entity](surfaces httpx.Surfaces, s Spec[T], verb string, c *SessionCommand[I, O], load func(context.Context, db.Tx[db.Tenant], uuid.UUID, uuid.UUID) (entity.GuidedSession, error)) {
	if c == nil {
		return
	}
	opts := c.Options
	opts.Auth = opts.Auth.RecheckingReplay(func(ctx context.Context, tx db.Tx[db.Tenant]) (bool, error) {
		req, ok := httpx.RequestFrom(ctx)
		if !ok {
			return false, nil
		}
		path := strings.TrimSuffix(req.URL.Path, "/"+verb)
		id, err := uuid.Parse(path[strings.LastIndex(path, "/")+1:])
		if err != nil {
			return false, nil
		}
		in, err := sessionRequest[I](req)
		if err != nil {
			return false, nil
		}
		actor, _ := tenancy.PrincipalFrom(ctx)
		return c.Recheck(ctx, tx, actor.UserID, id, in)
	})
	CommandResult(surfaces, s, verb, verb, "", c.Events,
		func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, in I) (Response[O], error) {
			var zero Response[O]
			if err := sessionInput(in); err != nil {
				return zero, err
			}
			actor, _ := tenancy.PrincipalFrom(ctx)
			out, err := c.Run(ctx, tx, actor.UserID, id, in)
			if err != nil {
				return zero, err
			}
			current, err := load(ctx, tx, actor.UserID, id)
			if err != nil {
				return zero, err
			}
			if !sessionResult(current, in, out) {
				return zero, fmt.Errorf("session: command result does not match retained progress")
			}
			return Response[O]{Result: out}, nil
		}, opts)
}

func sessionInput(in any) error {
	var revision int64
	switch v := in.(type) {
	case entity.CompleteGuidedSet:
		if v.StepID == uuid.Nil || v.SetID == uuid.Nil {
			return invalid("stepId", "and setId are required")
		}
		revision = v.IfRevision
	case entity.FinishGuidedSession:
		revision = v.IfRevision
	}
	if revision < 1 || revision > 1<<53-1 {
		return invalid("ifRevision", "must be a positive exact revision")
	}
	return nil
}

func sessionResult(s entity.GuidedSession, in, out any) bool {
	if s.Progress == nil {
		return false
	}
	switch v := in.(type) {
	case entity.CompleteGuidedSet:
		for _, c := range s.Progress.Completed {
			if c.StepID == v.StepID && c.SetID == v.SetID {
				return c.Revision == v.IfRevision+1 && reflect.DeepEqual(c, out)
			}
		}
	case entity.FinishGuidedSession:
		return s.Progress.Finished != nil && s.Progress.Finished.Revision == v.IfRevision+1 && reflect.DeepEqual(*s.Progress.Finished, out)
	}
	return false
}
