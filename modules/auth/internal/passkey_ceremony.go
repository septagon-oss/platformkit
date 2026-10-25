package internal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

// ceremonyRow is one in-flight ceremony as the table holds it.
type ceremonyRow struct {
	Kind      string
	UserID    *uuid.UUID
	Session   []byte
	ExpiresAt time.Time
}

// storeCeremony writes the nonce the answer must match. session is the library's
// own record of the ceremony — the challenge, the relying-party id it was begun
// under, the origin — and it is stored rather than rebuilt so a response collected
// at another origin, or under another id, fails verification against what was
// actually promised rather than against what this request happens to configure now.
func (s *Service) storeCeremony(ctx context.Context, tx db.Tx[db.Tenant], kind string, userID *uuid.UUID, session *webauthn.SessionData, options any) (*contracts.PasskeyChallenge, error) {
	blob, err := json.Marshal(session)
	if err != nil {
		return nil, fmt.Errorf("auth: encode a passkey ceremony: %w", err)
	}
	body, err := json.Marshal(options)
	if err != nil {
		return nil, fmt.Errorf("auth: encode a passkey ceremony's options: %w", err)
	}
	id, at := uuid.New(), db.Now()
	res := tx.DB().Exec(
		"INSERT INTO passkey_challenges (id, tenant_id, user_id, kind, session, expires_at, created_at)"+
			" VALUES (?, ?, ?, ?, ?::jsonb, ?, ?)",
		id, db.TenantOf(tx).ID, userID, kind, blob, at.Add(contracts.PasskeyChallengeWindow), at)
	if res.Error != nil {
		return nil, fmt.Errorf("auth: begin a passkey ceremony: %w", res.Error)
	}
	return &contracts.PasskeyChallenge{Ceremony: id, Options: body}, nil
}

// spendCeremony deletes the row a ceremony answer is being made against and reports
// whether this request is the one that got it, with the door the row names.
//
// The caller names the doors this answer is good at, and the row's own kind is the
// thing compared: a second-factor prompt is spendable at the second-factor answer
// and nowhere else, and the assertion leg names its two sign-in doors rather than
// "anything" — a register ceremony is not a sign-in waiting to be answered, and a
// list a caller forgot to fill matches nothing rather than everything.
//
// A body aimed at the wrong door consumes nothing — the DELETE asks for the kind
// the row itself reported, and the row survives for the door it was minted at.
//
// The delete is a transaction of its own, which is the one new write outside the
// request's transaction in this delivery, and markFirstFactorProved's reasoning is
// the reason: kit/httpx does not commit a request that answers 401, and 401 is the
// ordinary answer at a public ceremony leg, so a DELETE in the caller's transaction
// would be undone by the refusal it was meant to consume. A nonce is not account
// state — consuming it changes nothing about anybody's factors, writes no event and
// returns no session — so "a refused mutation writes nothing" still holds.
//
// A failure is logged and the answer is unchanged: the person is refused and begins
// again, which costs one tap.
func (s *Service) spendCeremony(ctx context.Context, tx db.Tx[db.Tenant], ceremony uuid.UUID, wantUser *uuid.UUID, wantKinds ...string) (*webauthn.SessionData, string, bool, error) {
	var row ceremonyRow
	err := tx.DB().Raw(
		"SELECT kind, user_id, session, expires_at FROM passkey_challenges WHERE id = ?", ceremony,
	).Scan(&row).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, "", false, fmt.Errorf("auth: read a passkey ceremony: %w", err)
	}
	if !slices.Contains(wantKinds, row.Kind) {
		return nil, "", false, nil
	}
	if wantUser != nil && (row.UserID == nil || *row.UserID != *wantUser) {
		return nil, "", false, nil
	}
	if !row.ExpiresAt.After(db.Now()) {
		return nil, "", false, nil
	}
	if !spendCeremonyRow(ctx, ceremony, row.Kind, wantUser) {
		return nil, "", false, nil
	}
	session := &webauthn.SessionData{}
	if err := json.Unmarshal(row.Session, session); err != nil {
		return nil, "", false, fmt.Errorf("auth: decode a passkey ceremony: %w", err)
	}
	return session, row.Kind, true, nil
}

// spendCeremonyRow is the consuming DELETE, outside the request's transaction.
// It asks for the kind the row itself reported, so the DELETE is a compare-and-
// delete against the row this request read rather than against an argument.
func spendCeremonyRow(ctx context.Context, ceremony uuid.UUID, kind string, wantUser *uuid.UUID) bool {
	conn, ok := httpx.ConnFrom(ctx)
	if !ok {
		return false
	}
	detached, cancel := context.WithTimeout(db.Detached(context.WithoutCancel(ctx)), detachedWriteBudget)
	defer cancel()
	var spent int64
	err := db.Run(detached, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		q := tx.DB().Exec(
			"DELETE FROM passkey_challenges WHERE id = ? AND kind = ? AND user_id IS NOT DISTINCT FROM ?"+
				" AND expires_at > now()",
			ceremony, kind, wantUser)
		spent = q.RowsAffected
		return q.Error
	})
	if err != nil {
		slog.ErrorContext(ctx, "auth: could not spend a passkey ceremony", "ceremony", ceremony, "error", err)
		return false
	}
	return spent == 1
}
