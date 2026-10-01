package internal

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

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

// RevokeAllSessions is the revocation that keeps nothing, and it is revoke with
// the nil UUID as the session to keep — so the "everywhere" answer and the
// password change's clause are one statement, one count and one event per row.
func (s *Service) RevokeAllSessions(ctx context.Context, tx db.Tx[db.Tenant], userID uuid.UUID) (int, error) {
	return s.revoke(ctx, tx, userID, uuid.Nil)
}

// revoke ends every session of this user that is not the session named, and
// publishes one auth.session_revoked per row it removed, with All set only when
// nothing was kept. Each event names the machine that left — which is what the
// trail is for — so the agent, the address and the expiry are read out of the row
// as it goes rather than reconstructed afterwards.
//
// Keeping the nil UUID's hash is how "keep none" is said: no session hashes to
// the digest of the zero UUID, so the clause is a tautology that costs no branch,
// and the two commands above cannot drift into two statements with two behaviours.
//
// Both revocations publish, because both are revocations. The password change was
// once the exception — the clause argument was that it is not a decision anybody
// made on its own — and that argument inverted the moment the sessions screen put
// an "end every session but this one" button behind this command: a person
// deciding on their own, enclosed by nothing, on a surface whose JSON sibling
// publishes per row. A state change that leaves no record is a state change the
// trail cannot show, whichever command asked for it; the password change is now on
// the trail too, one row per machine it signed out, next to its own change.
//
// The read and the delete are one statement, and that is the whole of the
// concurrency claim. Read-then-delete, two of these racing each other was not
// idempotent: the loser's list was the state as it was a moment before, its
// DELETE affected nothing, and it still published one revocation per row its
// snapshot happened to name and returned that count — so the trail said a
// machine left that nobody removed, twice. `DELETE … RETURNING` reports what
// this transaction actually deleted, which is the same fact the one-session
// command gets out of its row lock, from the statement that did the work. The
// loser of that race now takes no rows, publishes nothing and reports none, and
// nothing locks for the purpose: a row this command deletes is not a row another
// command can be revising.
func (s *Service) revoke(ctx context.Context, tx db.Tx[db.Tenant], userID, except uuid.UUID) (int, error) {
	type left struct {
		ref    string
		agent  string
		ip     string
		expiry time.Time
	}
	all := except == uuid.Nil
	rows, err := tx.DB().Raw(
		"DELETE FROM sessions WHERE user_id = ? AND id_hash <> ? RETURNING id_hash, user_agent, ip, expires_at",
		userID, contracts.Hash(except.String()),
	).Rows()
	if err != nil {
		return 0, fmt.Errorf("auth: revoke the sessions of %s: %w", userID, err)
	}
	// Collected before anything is published: the cursor holds this transaction's
	// connection, and the outbox insert is another statement on it.
	var gone []left
	for rows.Next() {
		var (
			hash []byte
			row  left
		)
		if err := rows.Scan(&hash, &row.agent, &row.ip, &row.expiry); err != nil {
			rows.Close()
			return 0, fmt.Errorf("auth: read the sessions it revoked: %w", err)
		}
		row.ref = hex.EncodeToString(hash)
		gone = append(gone, row)
	}
	if err := rows.Close(); err != nil {
		return 0, fmt.Errorf("auth: revoke the sessions of %s: %w", userID, err)
	}
	at := db.Now()
	for _, row := range gone {
		err := events.Publish(ctx, tx, contracts.EventSessionRevoked, contracts.SessionRevoked{
			UserID: userID, SessionRef: row.ref,
			UserAgent: row.agent, IP: row.ip, ExpiresAt: row.expiry, All: all, At: at,
		})
		if err != nil {
			return 0, err
		}
	}
	return len(gone), nil
}
