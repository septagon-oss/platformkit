// Package postgres supplies explicit PostgreSQL adapters for task resolution.
package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/task/internal/resolutionsql"
	"github.com/septagon-oss/platformkit/modules/task/resolution"
)

type resolutionStore struct{ conn *db.Conn }

func NewResolutionStore(conn *db.Conn) (resolution.AtomicStore, error) {
	if conn == nil {
		return nil, fmt.Errorf("task postgres: a connection is required")
	}
	return &resolutionStore{conn: conn}, nil
}

func (s *resolutionStore) CommitLockedResolution(ctx context.Context, tenant tenancy.Tenant, id uuid.UUID, actor tenancy.PolicyActor, apply func(resolution.LockedTask) error) error {
	ctx, err := actorContext(ctx, actor)
	if err != nil {
		return err
	}
	return db.RunOwned(ctx, s.conn, tenant, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		locked, err := resolutionsql.Lock(ctx, tx, id)
		if err != nil {
			return err
		}
		return apply(locked)
	})
}

// LockResolution binds the caller's transaction and explicit audit actor.
// It does not authorize or commit; check current authority before using
// resolution.StageAuthorized, then commit or roll back the enclosing operation.
func LockResolution(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, actor tenancy.PolicyActor) (resolution.LockedTask, error) {
	ctx, err := actorContext(ctx, actor)
	if err != nil {
		return nil, err
	}
	return resolutionsql.Lock(ctx, tx, id)
}

func actorContext(ctx context.Context, actor tenancy.PolicyActor) (context.Context, error) {
	var id uuid.UUID
	valid := false
	switch actor.Kind {
	case tenancy.PolicyUser:
		var err error
		id, err = uuid.Parse(actor.ID)
		valid = err == nil && id != uuid.Nil
	case tenancy.PolicySystem:
		valid = strings.TrimSpace(actor.ID) != ""
	case tenancy.PolicyPublic:
		valid = actor.ID == ""
	}
	if !valid {
		return nil, fmt.Errorf("%w: postgres task actors need a user UUID, named system or anonymous public identity", tenancy.ErrInvalidPolicyRequest)
	}
	// Existing outbox Actor stores user UUIDs only. System/public calls carry
	// no user attribution; explicitly clear any inherited user's context value.
	return tenancy.WithActor(ctx, id), nil
}
