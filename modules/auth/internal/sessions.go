package internal

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

// lockForUpdate is the row lock RevokeSession takes before it decides — the
// same clause crud.GetForUpdate applies, for the same reason. The sessions table
// is keyed by a hash and carries no deleted_at, so it is not a crud.Entity and
// the helper cannot be used here; this is the one place that difference shows.
var lockForUpdate = clause.Locking{Strength: "UPDATE"}

// Sessions is the list a person reads to answer "was that me?", in the caller's
// transaction and so under this tenant's policy: another tenant's sessions are
// not rows this query can see, which is why nothing here compares a tenant id.
//
// The rows come back as refs, not as sessions (contracts.SessionListing says
// why), and the two lifetimes are applied here rather than in the WHERE clause
// for the reason Identify gives for the same choice: a row that has passed one
// is refused, and listing it would be listing a session that is already gone.
func (s *Service) Sessions(_ context.Context, tx db.Tx[db.Tenant], userID, current uuid.UUID) ([]*contracts.SessionListing, error) {
	var rows []contracts.Session
	err := tx.DB().Where("user_id = ?", userID).
		Order("last_seen_at DESC, created_at DESC").Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("auth: list the sessions of %s: %w", userID, err)
	}
	at := db.Now()
	mine := contracts.SessionRef(current)
	out := make([]*contracts.SessionListing, 0, len(rows))
	for _, row := range rows {
		if !row.ExpiresAt.After(at) || !row.CreatedAt.Add(contracts.SessionMaxLifetime).After(at) {
			continue
		}
		ref := hex.EncodeToString(row.IDHash)
		out = append(out, &contracts.SessionListing{
			Ref: ref, UserAgent: row.UserAgent, IP: row.IP,
			CreatedAt: row.CreatedAt, LastSeenAt: row.LastSeenAt, ExpiresAt: row.ExpiresAt,
			Current: ref == mine,
		})
	}
	return out, nil
}

// RevokeSession ends one of this person's own sessions, by its ref.
//
// The read takes FOR UPDATE for the same reason a command locks the row it is
// about to change: without it two tabs that both click revoke on the same
// session both see the row, both delete (one affecting a row, one none), and
// both publish — so the trail would say the machine left twice. Locked, the
// second settles after the first, finds no row, and refuses with no write and
// no event.
//
// The ref must belong to this person as well as to this tenant. A ref that is
// somebody else's session, another tenant's session, or no session at all is
// one answer — crud.ErrNotFound — because "which of those was it" is exactly the
// question a leaked ref would let somebody ask.
func (s *Service) RevokeSession(ctx context.Context, tx db.Tx[db.Tenant], userID uuid.UUID, ref string) error {
	hash, err := contracts.SessionRefOf(ref)
	if err != nil {
		return crud.ErrNotFound
	}
	var session contracts.Session
	err = tx.DB().Clauses(lockForUpdate).Where("id_hash = ? AND user_id = ?", hash, userID).Take(&session).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return crud.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("auth: read the session to revoke it: %w", err)
	}
	if err := tx.DB().Where("id_hash = ?", hash).Delete(&contracts.Session{}).Error; err != nil {
		return fmt.Errorf("auth: revoke the session: %w", err)
	}
	return events.Publish(ctx, tx, contracts.EventSessionRevoked, contracts.SessionRevoked{
		UserID: userID, SessionRef: ref, UserAgent: session.UserAgent, IP: session.IP,
		ExpiresAt: session.ExpiresAt, At: db.Now(),
	})
}

// RevokeAllSessions ends every session this person has and publishes one event
// per row it removed, with All set. The rows are read before they go for that
// reason and for no other: the event says which machine left, and the agent and
// the address live in the row.
//
// There is no `except`, so the person who asked is signed out of the page they
// asked on — which is what "everywhere" means, and what the route then makes
// obvious by clearing the cookie and sending them to the sign-in page.
//
// Two of these racing each other is idempotent: both delete the same rows and
// the loser publishes nothing, because the DELETE that found nothing has
// nothing to report. Nothing locks here for that reason; a row this command
// deletes is not a row another command can be revising.
func (s *Service) RevokeAllSessions(ctx context.Context, tx db.Tx[db.Tenant], userID uuid.UUID) (int, error) {
	var rows []contracts.Session
	if err := tx.DB().Where("user_id = ?", userID).Find(&rows).Error; err != nil {
		return 0, fmt.Errorf("auth: read the sessions to revoke them: %w", err)
	}
	if len(rows) == 0 {
		return 0, nil
	}
	if err := tx.DB().Where("user_id = ?", userID).Delete(&contracts.Session{}).Error; err != nil {
		return 0, fmt.Errorf("auth: revoke every session of %s: %w", userID, err)
	}
	at := db.Now()
	for _, row := range rows {
		err := events.Publish(ctx, tx, contracts.EventSessionRevoked, contracts.SessionRevoked{
			UserID: userID, SessionRef: hex.EncodeToString(row.IDHash),
			UserAgent: row.UserAgent, IP: row.IP, ExpiresAt: row.ExpiresAt, All: true, At: at,
		})
		if err != nil {
			return 0, err
		}
	}
	return len(rows), nil
}
