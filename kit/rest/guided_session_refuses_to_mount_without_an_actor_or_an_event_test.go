package rest_test

// A guided session mounts no table: the module's read answers the row, and every
// route it registers — the read and each command — sits on the app surface and takes
// its actor from the request. A guard that admits nobody therefore hands the module's
// Load, and a command's Run, the zero actor rather than a person, and a command that
// declares no event records progress with nothing to commit beside it. checkSession
// is where either refusal has to land, before a route exists to serve it.

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
)

// TestAGuidedSessionRefusesToMountWithoutAnActorOrAnEvent mounts a session whose
// halves are otherwise one the mount admits, and refuses the three that cannot say
// who the person is or what its write emits. A read guarded by httpx.Public and a
// command guarded the same way would serve the zero actor; a command with no
// declared event would keep a set no outbox row names.
func TestAGuidedSessionRefusesToMountWithoutAnActorOrAnEvent(t *testing.T) {
	run := func(context.Context, db.Tx[db.Tenant], uuid.UUID, uuid.UUID, entity.CompleteGuidedSet) (entity.GuidedCompletion, error) {
		return entity.GuidedCompletion{}, nil
	}
	recheck := func(context.Context, db.Tx[db.Tenant], uuid.UUID, uuid.UUID, entity.CompleteGuidedSet) (bool, error) {
		return true, nil
	}
	session := func() rest.SessionSpec {
		return rest.SessionSpec{Module: "practice", Entity: "session", Path: "/sessions",
			Read: "practice:read", Operations: []httpx.CRUD{rest.Read},
			Present: entity.EntryHints{Session: true},
			Session: &rest.SessionRead{Load: func(context.Context, db.Tx[db.Tenant], uuid.UUID, uuid.UUID) (entity.GuidedSession, error) {
				return entity.GuidedSession{}, nil
			}}}
	}
	refused := func(why string, change func(*rest.SessionSpec)) func(*testing.T) {
		return func(t *testing.T) {
			s := session()
			change(&s)
			hintRefused(t, func() { s.Mount(httpx.Surfaces{}) }, why)
		}
	}
	t.Run("the read guard admits nobody", refused("authenticated app guard",
		func(s *rest.SessionSpec) { s.ReadAuth = httpx.Public() }))
	t.Run("the command guard admits nobody", refused("one authenticated Auth",
		func(s *rest.SessionSpec) {
			s.Session.Complete = &rest.SessionCommand[entity.CompleteGuidedSet, entity.GuidedCompletion]{
				Options: rest.CommandOptions{Auth: httpx.Public()}, Events: []string{"practice.set.completed"}, Run: run, Recheck: recheck}
		}))
	t.Run("the command declares no event", refused("must declare domain events",
		func(s *rest.SessionSpec) {
			s.Session.Complete = &rest.SessionCommand[entity.CompleteGuidedSet, entity.GuidedCompletion]{
				Options: rest.CommandOptions{Auth: httpx.Permission("practice:complete")}, Run: run, Recheck: recheck}
		}))
}
