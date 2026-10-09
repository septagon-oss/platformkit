// This file is this module's answer to one contract: personaldata.Data (ADR 0020).
//
// Export is a read of the rows this module owns about one person, in the shape this
// module's own contracts use. Erase removes what those rows *say* and keeps the rows
// wherever the audit trail has to keep a row; the person's id survives, the strings
// that named them do not. Both run inside the caller's transaction, inside the tenant
// row-level security already bounded, so neither names tenant_id.
//
// It is a contributor and never a command: no code in this module calls it. The module
// that composes the parts (modules/subject) does, and the kernel reads the same
// declaration below off the manifest to refuse a column this file does not scrub.
package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/personaldata"
)

// personalData is the credential half of a person. Its rule is the asymmetry ADR 0020
// §4 draws: what a person *knows* is removed, not anonymised — a reset token, a TOTP
// secret, a recovery code, an API token and a first-factor proof that outlived their
// erasure are each a door with somebody else's name on it — and what a person *did`
// stays: the session row, with its device string and its address scrubbed.
type personalData struct{}

// PersonalData is this module's contribution to a person's export and erasure.
func PersonalData() personaldata.Part { return personaldata.Part{Module: "auth", Data: personalData{}} }

type authPart struct {
	Sessions          []authSessionRow   `json:"sessions"`
	APITokens         []authApiTokenRow  `json:"apiTokens"`
	Factors           []authFactorRow    `json:"totpFactors"`
	RecoveryCodes     []authRecoveryRow  `json:"recoveryCodes"`
	PasswordTokens    []authIssuedRow    `json:"passwordTokens"`
	VerificationEmail []authVerification `json:"authVerificationEmails"`
	FirstFactorProofs []authProofRow     `json:"firstFactorProofs"`
}

// authSessionRow names no session. A session's id is its credential, and migration
// 000013 stopped storing it: the row is keyed by sha256 of the value the cookie holds,
// so there is nothing to hand back and a bundle that carried a hash would carry a
// guessable-at-brute-force copy of a live door. The dates, the browser and the address
// are the part a person recognises as "that login from the laptop".
type authSessionRow struct {
	CreatedAt  time.Time `json:"createdAt"`
	ExpiresAt  time.Time `json:"expiresAt"`
	LastSeenAt time.Time `json:"lastSeenAt"`
	UserAgent  string    `json:"userAgent"`
	IP         string    `json:"ip"`
}

// authApiTokenRow names a token and never carries one: no hash, no scope list a caller
// could replay, only what a person would recognise as "the key I made in March".
type authApiTokenRow struct {
	ID         uuid.UUID  `json:"id"`
	Name       string     `json:"name"`
	CreatedBy  string     `json:"createdBy"`
	Scopes     string     `json:"scopes"`
	CreatedAt  time.Time  `json:"createdAt"`
	ExpiresAt  time.Time  `json:"expiresAt"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
	RevokedAt  *time.Time `json:"revokedAt,omitempty"`
}

type authFactorRow struct {
	ID        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"createdAt"`
	Kind      string    `json:"kind"`
}

type authRecoveryRow struct {
	ID        uuid.UUID  `json:"id"`
	CreatedAt time.Time  `json:"createdAt"`
	UsedAt    *time.Time `json:"usedAt,omitempty"`
}

type authIssuedRow struct {
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type authVerification struct {
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type authProofRow struct {
	ExpiresAt time.Time `json:"expiresAt"`
}

func (personalData) Export(ctx context.Context, tx db.Tx[db.Tenant],
	s personaldata.Subject) (json.RawMessage, error) {
	var out authPart
	db_ := tx.DB().WithContext(ctx)
	q := func(dest any, table, columns, order string) error {
		return db_.Table(table).Select(columns).Where("user_id = ?", s.ID).Order(order).Find(dest).Error
	}
	if err := q(&out.Sessions, "sessions",
		"created_at, expires_at, last_seen_at, user_agent, ip", "created_at"); err != nil {
		return nil, fmt.Errorf("auth: export %s's sessions: %w", s.ID, err)
	}
	if err := q(&out.APITokens, "api_tokens",
		"id, name, created_by::text AS created_by, scopes::text AS scopes, created_at, expires_at, last_used_at, revoked_at",
		"created_at, id"); err != nil {
		return nil, fmt.Errorf("auth: export %s's API tokens: %w", s.ID, err)
	}
	if err := q(&out.Factors, "totp_factors", "id, created_at, 'totp' AS kind", "created_at, id"); err != nil {
		return nil, fmt.Errorf("auth: export %s's factors: %w", s.ID, err)
	}
	if err := q(&out.RecoveryCodes, "recovery_codes", "id, created_at, used_at", "created_at, id"); err != nil {
		return nil, fmt.Errorf("auth: export %s's recovery codes: %w", s.ID, err)
	}
	if err := q(&out.PasswordTokens, "password_tokens", "created_at, expires_at", "created_at"); err != nil {
		return nil, fmt.Errorf("auth: export %s's password tokens: %w", s.ID, err)
	}
	if err := q(&out.VerificationEmail, "verification_tokens", "email, created_at, expires_at", "created_at"); err != nil {
		return nil, fmt.Errorf("auth: export %s's address verifications: %w", s.ID, err)
	}
	if err := q(&out.FirstFactorProofs, "first_factor_proofs", "expires_at", "expires_at"); err != nil {
		return nil, fmt.Errorf("auth: export %s's first-factor proofs: %w", s.ID, err)
	}
	return json.Marshal(out)
}

// Erase ends the sessions, then removes every credential, then scrubs what stayed.
//
// The order is the reason it is three statements: the scrub's own re-read (ADR 0020 §4)
// asks whether any declared text column still names the person, and a row about to be
// deleted has no reason to be scrubbed first. Ending a session rather than deleting it
// keeps the one record of "this account was signed in from there" that the tenant may
// still need, with the browser string and the address removed from it.
func (personalData) Erase(ctx context.Context, tx db.Tx[db.Tenant], s personaldata.Subject) error {
	db_ := tx.DB().WithContext(ctx)
	if err := db_.Exec("UPDATE sessions SET expires_at = ? WHERE user_id = ? AND expires_at > ?",
		db.Now(), s.ID, db.Now()).Error; err != nil {
		return fmt.Errorf("auth: end %s's sessions: %w", s.ID, err)
	}
	// verification_tokens and not a name of this file's inventing: it is the table
	// modules/auth/migrations/000024_email_verification.up.sql creates.
	for _, t := range []string{"password_tokens", "verification_tokens", "totp_factors", "recovery_codes", "api_tokens", "first_factor_proofs"} {
		// The table name is this file's own constant, never a caller's: it is the only
		// thing a name written into SQL could be.
		if err := db_.Exec("DELETE FROM "+t+" WHERE user_id = ?", s.ID).Error; err != nil {
			return fmt.Errorf("auth: remove %s's %s: %w", s.ID, t, err)
		}
	}
	if _, err := personaldata.Scrub(ctx, tx, s, "sessions", []string{"user_agent", "ip"},
		"user_id = ?", s.ID); err != nil {
		return err
	}
	// A token another person minted for this one is removed above; a token this person
	// minted for themselves is the same row. What is left in created_by is an id, which
	// is the trail's and not a name.
	return nil
}
