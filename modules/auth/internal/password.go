package internal

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	notificationcontracts "github.com/septagon-oss/platformkit/modules/notification/contracts"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// ChangePassword is the signed-in half of "I want a different password".
//
// It asks for the current one, and that is the whole point of the route: a
// session cookie is something a browser attaches, so without this check a
// stolen cookie is a stolen account rather than a stolen session. It ends the
// other sessions in the same transaction, because a new password that leaves
// the old one's sessions working has not replaced anything.
func (s *Service) ChangePassword(ctx context.Context, tx db.Tx[db.Tenant], userID, keep uuid.UUID, current, next string) error {
	user, err := s.users.Get(ctx, tx, userID)
	if err != nil {
		return err
	}
	if !user.CanSignIn() || !user.CheckPassword(current) {
		return contracts.ErrCredentials
	}
	if err := s.users.SetPassword(ctx, tx, userID, next); err != nil {
		return err
	}
	return s.RevokeSessions(ctx, tx, userID, keep)
}

// Forget issues a reset token for an address and mails the link.
//
// Every path through it returns nil. An address nobody has, a deactivated user,
// a composition with no notifier: all of them are the same answer, because the
// route this sits behind is public and one that said "no such address" would be
// an account enumeration oracle anybody could run. The cost of that honesty is
// that a person who mistypes their own address is told nothing; the mail that
// does not arrive is the message.
func (s *Service) Forget(ctx context.Context, tx db.Tx[db.Tenant], email string) error {
	user, err := s.users.ByEmail(ctx, tx, email)
	switch {
	case errors.Is(err, crud.ErrNotFound):
		return nil
	case err != nil:
		return err
	case user.Status == usercontracts.StatusInactive:
		// A deactivated account is not one somebody may talk their way back
		// into, and saying so would be the oracle again.
		return nil
	}
	return s.offer(ctx, tx, user, resetSubject, resetBody)
}

// Offer issues a set-password token for somebody who has just been invited.
//
// It is the whole body of the user.invited subscription, and it is the same
// token Forget issues: an invitation and a reset are one fact — somebody who
// cannot sign in has been sent a link that lets them choose a password once —
// and two mechanisms would be two expiries to keep in step.
//
// A user who already has a password is skipped. user.invited is published by
// the bootstrap's Provision as well as by Invite, and the first administrator
// of an installation has a password already, printed on the terminal.
func (s *Service) Offer(ctx context.Context, tx db.Tx[db.Tenant], userID uuid.UUID) error {
	user, err := s.users.Get(ctx, tx, userID)
	if errors.Is(err, crud.ErrNotFound) {
		return nil
	}
	if err != nil || user.Status != usercontracts.StatusInvited {
		return err
	}
	return s.offer(ctx, tx, user, inviteSubject, inviteBody)
}

// offer writes the token and asks for the mail.
//
// The row holds sha256 of the token and the token goes only into the message,
// so a copy of this table is a list of hashes. ON CONFLICT on (tenant_id,
// user_id) is the single-pending rule: asking again replaces the last link
// rather than adding a second, so a mailbox with four of these mails still has
// one that works.
func (s *Service) offer(ctx context.Context, tx db.Tx[db.Tenant], user *usercontracts.User, title, body string) error {
	if s.notify == nil {
		// A composition with no notifier writes no token either: a link nobody
		// is sent is a live credential in a table for an hour, for nothing.
		slog.WarnContext(ctx, "auth: no notifier is wired, so no set-password link was sent", "user", user.ID)
		return nil
	}
	token := secret()
	at := db.Now()
	err := tx.DB().Exec(
		"INSERT INTO password_tokens (token_hash, tenant_id, user_id, created_at, expires_at) VALUES (?, ?, ?, ?, ?)"+
			" ON CONFLICT (tenant_id, user_id) DO UPDATE SET token_hash = EXCLUDED.token_hash,"+
			" created_at = EXCLUDED.created_at, expires_at = EXCLUDED.expires_at",
		contracts.Hash(token), db.TenantOf(tx).ID, user.ID, at, at.Add(contracts.TokenLifetime)).Error
	if err != nil {
		return fmt.Errorf("auth: issue a password token: %w", err)
	}
	_, err = s.notify.Notify(ctx, tx, notificationcontracts.Notice{
		Recipient: user.ID, Title: title,
		Body: body + "\n\nThe link works once and stops working in an hour.",
		// A path and not a URL: the notification module refuses an absolute one,
		// and the tenant's own host is what the mail turns it into.
		Link:  ResetPath + "?token=" + token,
		Email: true,
	})
	return err
}

// Reset consumes a token, sets the password and ends every session.
//
// Every one, including any the caller holds: whoever is resetting a password
// has already shown they were not relying on a session, and whoever else held
// one may be the reason it is being reset. The row is deleted rather than
// flagged, so "used once" is the row being gone — two requests racing on one
// token is one DELETE returning a row and one returning none, decided by
// Postgres rather than by a read and a write this code would have to get right.
func (s *Service) Reset(ctx context.Context, tx db.Tx[db.Tenant], token, password string) error {
	if token == "" {
		return contracts.ErrCredentials
	}
	// The lookup is by the hash of what was presented, which is a primary-key
	// probe on a value an attacker cannot steer: the token is 256 bits of
	// crypto/rand, so there is no prefix to walk and nothing a timing
	// difference on the index would narrow. What the hash buys is the other
	// thing — a copy of this table is not a set of live links.
	var userID uuid.UUID
	err := tx.DB().Raw(
		"DELETE FROM password_tokens WHERE token_hash = ? AND expires_at > now() RETURNING user_id",
		contracts.Hash(token)).Row().Scan(&userID)
	switch {
	case errors.Is(err, sql.ErrNoRows), errors.Is(err, gorm.ErrRecordNotFound):
		// An unknown token, a spent one and an expired one are one answer, for
		// the reason Login's three refusals are one answer.
		return contracts.ErrCredentials
	case err != nil:
		return fmt.Errorf("auth: consume a password token: %w", err)
	}
	if err := s.users.SetPassword(ctx, tx, userID, password); err != nil {
		return err
	}
	if err := s.RevokeSessions(ctx, tx, userID, uuid.Nil); err != nil {
		return err
	}
	return events.Publish(ctx, tx, contracts.EventPasswordReset, contracts.PasswordReset{
		UserID: userID, At: db.Now(),
	})
}

// The two messages. They are here rather than in a template because the
// notification module's template is the envelope — a title, a body and a link —
// and what goes in it belongs to whoever raised the notice.
const (
	inviteSubject = "Set your password"
	inviteBody    = "Somebody invited you. Follow the link to choose a password and sign in."
	resetSubject  = "Reset your password"
	resetBody     = "Somebody asked to reset the password for this address. If it was not you, ignore this message and nothing changes."
)

// ResetPath is where the link points. It is a path within the application, so a
// notice can never send a tenant's people somewhere else, and the host the mail
// turns it into is that tenant's own.
const ResetPath = "/auth/reset"

// secret is 32 bytes of crypto/rand, base64url. It only has to be unguessable
// and unique, and it is never stored: the row holds its hash.
func secret() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand does not fail on any platform this runs on, and a
		// predictable token would be an account anybody could take.
		panic("auth: no randomness: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
