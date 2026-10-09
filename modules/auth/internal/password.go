package internal

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
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

// Forget publishes auth.reset_requested, and that is the whole of the request.
//
// No lookup, no token, no mail: those are Reissue's, in the worker. The route
// this sits behind is public and has to cost the same whether or not anybody
// has the address, and doing the lookup here did not — a known address answered
// in 2.1 ms and an unknown one in 0.9 ms, two distributions that did not
// overlap, which is an account enumeration oracle with a stopwatch. One INSERT
// into the outbox is the same INSERT either way.
//
// The cost of that honesty is unchanged and worth restating: a person who
// mistypes their own address is told nothing, and the mail that does not arrive
// is the message.
func (s *Service) Forget(ctx context.Context, tx db.Tx[db.Tenant], email string) error {
	r, _ := httpx.RequestFrom(ctx)
	return events.Publish(ctx, tx, contracts.EventResetRequested, contracts.ResetRequested{
		Email: contracts.EmailKey(email), At: db.Now(), Served: httpx.ServedAuthority(r),
	})
}

// Reissue is the worker's half of the forgotten-password flow: the lookup the
// request refused to do, done where no stopwatch can reach it.
//
// Every path returns nil. An address nobody has, a deactivated account, a
// composition with no mailer, a person who was sent a link a moment ago: none
// of those is a failure the outbox should retry four times and dead-letter, and
// none of them is anything a stranger gets to measure.
func (s *Service) Reissue(ctx context.Context, tx db.Tx[db.Tenant], email string) error {
	user, err := s.users.ByEmail(ctx, tx, email)
	switch {
	case errors.Is(err, crud.ErrNotFound):
		return nil
	case err != nil:
		return err
	case user.Status != usercontracts.StatusInvited && user.Status != usercontracts.StatusActive:
		// Recovery cannot bypass a required approval or restore a deactivated account.
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

// offer mints the token, sends the mail, and puts the credential back if the
// transport refused it.
//
// # Where the row sits, relative to the mail
//
// The row is written before the message is handed over, and a refused send puts it
// back rather than letting the attempt roll back — the attempt commits now, because
// the record of the refusal is written in the same transaction and a record that
// rolled back with the failure is a failure nobody can see.
//
// Both halves of that are load-bearing. A mailbox that already shows a link with no
// row behind it is a link that does not work yet: apps/platformkit's four-eyes
// journey redeems the token the moment the mail appears. And a committed failure may
// not hold a live credential nobody was sent — which is what the restore is for, and
// it leaves recent() counting the link that actually left, so the person can ask
// again at once instead of being answered by a cap they were never mailed under. And
// because the upsert replaces whatever row this attempt found, a refused resend would
// otherwise destroy the link the person was sent five minutes ago — which is why the
// restore writes the old hash, timestamp and expiry back rather than only deleting.
//
// The transport is the last thing before the commit that a successful attempt makes:
// one statement, the record of the send, runs after it. That is deliberate. A mail the
// person can already read in their mailbox is a link they may use at once, and every
// statement this transaction still owes is a moment in which that link answers "those
// credentials are not right".
//
// # Where the secret is, and where it is not
//
// The token exists in exactly two places: the message this hands the mail
// server, and sha256 of it in password_tokens. It is in no other row, which is
// a stronger claim than it sounds and the reason this function sends the mail
// itself rather than asking the notification module to.
//
// Everything else this application mails goes out of the notification worker,
// which reads the notification row back and renders it — so whatever is in the
// message is, by construction, in a row. A notification is an ordinary
// tenant-owned row: listed by a route, kept until somebody deletes it,
// readable by anybody who can read the table. Putting the link in
// notifications.link, which is what this used to do, made every reset link a
// live credential sitting in a table nobody treats as a credential store, and
// contradicted the property migrations/000014 states. The event cannot carry it
// either: an outbox row is kept for a week and modules/audit copies every
// payload into the audit trail.
//
// So the notice raised here carries ResetPath and nothing else — the person has
// something to see in the application, and it tells them to check their mail —
// and the secret goes straight from this transaction to the mail server. The
// composition wires notification's own Mailer, so there is still one sender.
//
// ON CONFLICT on (tenant_id, user_id) is the single-pending rule: asking again
// replaces the last link rather than adding a second, so a mailbox with four of
// these mails still has one that works. recent() is the other half of that —
// one link per person per ResetInterval, so an address somebody types
// repeatedly is one mail and not twenty.
// passwordLock serialises the two things that must not happen twice for one
// person: sending a second link inside the cap, and minting a token a concurrent
// attempt is about to replace. It is the same advisory lock verificationLock takes
// (verification.go), keyed the same way, and it is taken before recent() because
// recent() is a read: two attempts that both read "no link yet" would both mail.
// The key includes the tenant even though table visibility is already RLS-bound.
func passwordLock(tx db.Tx[db.Tenant], id uuid.UUID) error {
	key := "auth/password/" + db.TenantOf(tx).ID.String() + "/" + id.String()
	if err := tx.DB().Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", key).Error; err != nil {
		return fmt.Errorf("auth: lock a password link: %w", err)
	}
	return nil
}

// pendingLink is the credential row as one attempt found it, so that the attempt
// can put it back. The zero value means there was none.
type pendingLink struct {
	hash    contracts.Digest
	created time.Time
	expires time.Time
	there   bool
}

// pendingLinkOf reads the one outstanding link a person holds. It is called under
// the advisory lock, and it is the whole reason the lock is held across the send:
// a refused attempt has to restore this value rather than delete "the" row, because
// the row it replaced may be the only link that person has.
func pendingLinkOf(tx db.Tx[db.Tenant], table string, id uuid.UUID) (pendingLink, error) {
	var out pendingLink
	err := tx.DB().Raw("SELECT token_hash, created_at, expires_at FROM "+table+
		" WHERE tenant_id = ? AND user_id = ?", db.TenantOf(tx).ID, id).Row().
		Scan(&out.hash, &out.created, &out.expires)
	switch {
	case errors.Is(err, sql.ErrNoRows), errors.Is(err, gorm.ErrRecordNotFound):
		return out, nil
	case err != nil:
		return out, fmt.Errorf("auth: read the outstanding link in %s: %w", table, err)
	}
	out.there = true
	return out, nil
}

// restoreLink puts a person's credential row back the way this attempt found it.
// Two wrongs are refused here: leaving a live token that nobody was sent, and
// deleting the link that person was sent five minutes ago because a mail server
// refused them a second one.
func restoreLink(tx db.Tx[db.Tenant], table string, id uuid.UUID, was pendingLink) error {
	if !was.there {
		err := tx.DB().Exec("DELETE FROM "+table+" WHERE tenant_id = ? AND user_id = ?",
			db.TenantOf(tx).ID, id).Error
		if err != nil {
			return fmt.Errorf("auth: withdraw the link nobody was sent: %w", err)
		}
		return nil
	}
	err := tx.DB().Exec("UPDATE "+table+" SET token_hash = ?, created_at = ?, expires_at = ?"+
		" WHERE tenant_id = ? AND user_id = ?",
		was.hash, was.created, was.expires, db.TenantOf(tx).ID, id).Error
	if err != nil {
		return fmt.Errorf("auth: restore the link this attempt replaced: %w", err)
	}
	return nil
}

func (s *Service) offer(ctx context.Context, tx db.Tx[db.Tenant], user *usercontracts.User, title, body string) error {
	if s.mail.Mailer == nil {
		// A composition with no mailer writes no token either: a link nobody is
		// sent is a live credential in a table for an hour, for nothing. It leaves
		// a suppressed record, because the deployment rather than the caller is
		// what has to learn that no transport is wired.
		slog.WarnContext(ctx, "auth: no mailer is wired, so no set-password link was sent", "user", user.ID)
		return s.recordMail(ctx, tx, contracts.MailSetPassword, user.Email,
			notificationcontracts.MailSuppressed, "no mail transport is wired")
	}
	// Before the cap, so that an attempt waiting here reads the one that holds the
	// lock's committed token row instead of mailing the person twice.
	if err := passwordLock(tx, user.ID); err != nil {
		return err
	}
	recent, err := s.recent(tx, user.ID)
	if err != nil || recent {
		return err
	}
	was, err := pendingLinkOf(tx, "password_tokens", user.ID)
	if err != nil {
		return err
	}
	token, at := secret(), db.Now()
	err = tx.DB().Exec(
		"INSERT INTO password_tokens (token_hash, tenant_id, user_id, created_at, expires_at) VALUES (?, ?, ?, ?, ?)"+
			" ON CONFLICT (tenant_id, user_id) DO UPDATE SET token_hash = EXCLUDED.token_hash,"+
			" created_at = EXCLUDED.created_at, expires_at = EXCLUDED.expires_at",
		contracts.Hash(token), db.TenantOf(tx).ID, user.ID, at, at.Add(contracts.TokenLifetime)).Error
	if err != nil {
		return fmt.Errorf("auth: issue a password token: %w", err)
	}
	base, err := s.baseURL(ctx, tx)
	if err != nil {
		return err
	}
	if err := s.mail.Mailer.Send(ctx, mailMessage(user.Email, title, body, base, token)); err != nil {
		// The row goes back the way it came, and the attempt is acknowledged rather
		// than retried: the record of a refused mail is written in this transaction,
		// so this transaction has to commit for the record to exist (SPECIFY §3). A
		// committed failure may not hold a credential nobody was sent — which is what
		// the restore is for, and it also leaves the cap counting the link that was
		// really sent rather than this one, so the person can ask again at once.
		if err := restoreLink(tx, "password_tokens", user.ID, was); err != nil {
			return err
		}
		// And no notice is raised beside it: the in-app copy says "the link is in
		// the email this raised", and a committed sentence that promises a mail the
		// transport refused is the same lie the record exists to make impossible.
		// Before this branch's acknowledged failure the notice rolled back with the
		// attempt; raising it after the send is what keeps that promise now.
		return s.recordMail(ctx, tx, contracts.MailSetPassword, user.Email,
			notificationcontracts.MailFailed, err.Error(), token, string(contracts.Hash(token)))
	}
	if s.notify != nil {
		// Only now, and only because the transport took the message: a path, and one
		// with no query on it, because the notification module refuses an absolute
		// link and this one is not a credential at all.
		_, err = s.notify.Notify(ctx, tx, notificationcontracts.Notice{
			Recipient: user.ID, Title: title,
			Body: body + "\n\nThe link is in the email this raised. It works once and stops working in an hour.",
			Link: ResetPath,
		})
		if err != nil {
			return err
		}
	}
	return s.recordMail(ctx, tx, contracts.MailSetPassword, user.Email, notificationcontracts.MailSent, "")
}

// mailMessage renders the one message this module writes. The link is absolute
// and on the recipient's own tenant's host, because a mail client has no base to
// resolve a path against and one customer's people must not be sent to another's
// front door.
//
// The token is in this string and in nothing it returns to: the caller hands it
// straight to the mail server, and the record written beside the send names no
// subject, body, link or credential.
func mailMessage(to, title, body, base, token string) notificationcontracts.Message {
	return notificationcontracts.Message{
		To: to, Subject: title,
		Body: body + "\n\n" + base + ResetPath + "?token=" + token +
			"\n\nThe link works once and stops working in an hour.",
	}
}

// baseURL is the scheme and host this tenant's people reach the application at.
// A composition that wired no lookup mails a path, which is a link that works
// for nobody — so it is an error rather than a silent half-message.
func (s *Service) baseURL(ctx context.Context, tx db.Tx[db.Tenant]) (string, error) {
	if s.mail.Hosts == nil {
		return "", fmt.Errorf("auth: no host lookup is wired, so a mailed link would point nowhere")
	}
	host, err := s.mail.Hosts.PublicHost(ctx, tx)
	if err != nil {
		return "", fmt.Errorf("auth: find the host of %s: %w", db.TenantOf(tx).Slug, err)
	}
	if host == "" {
		return "", fmt.Errorf("auth: %s is served at no host, so a mailed link would point nowhere", db.TenantOf(tx).Slug)
	}
	scheme := "http"
	if s.mail.Secure {
		scheme = "https"
	}
	base := scheme + "://" + host
	// The port the request that raised this link was served at, when there was one
	// and when it is a port on this host of record: a development installation
	// serves a tenant at its name and a port, and a link that drops the port opens
	// a different server, or none. See served.go.
	if port := servedPort(ctx, host); port != "" {
		base += ":" + port
	}
	return base, nil
}

// recent reports whether this person was sent a link inside ResetInterval.
//
// It is the cap on outstanding notices per recipient, and it is read off the
// token row rather than counted anywhere else because the token table is
// already one row per person: asking again replaces the link, so the only thing
// left to bound is how many mails and how many notices that produces. Without
// it, a public route plus a known address is somebody else's inbox filled by a
// stranger, one mail per request.
func (s *Service) recent(tx db.Tx[db.Tenant], userID uuid.UUID) (bool, error) {
	var n int64
	err := tx.DB().Table("password_tokens").
		Where("user_id = ? AND created_at > now() - ?::interval", userID, resetInterval).
		Count(&n).Error
	if err != nil {
		return false, fmt.Errorf("auth: read the pending password token of %s: %w", userID, err)
	}
	return n > 0, nil
}

// resetInterval is contracts.ResetInterval as Postgres spells an interval, so
// the cutoff is one constant and the database applies it — the arrangement the
// purge's maxAge already uses.
var resetInterval = fmt.Sprintf("%d seconds", int(contracts.ResetInterval.Seconds()))

// Reset consumes a token, sets the password, ends every session, and lets the
// browser that spent the link in.
//
// Every session that existed when the link was spent ends, including any the
// caller holds: whoever is resetting a password has already shown they were not
// relying on a session, and whoever else held one may be the reason it is being
// reset. The row is deleted rather than flagged, so "used once" is the row being
// gone — two requests racing on one token is one DELETE returning a row and one
// returning none, decided by Postgres rather than by a read and a write this code
// would have to get right.
//
// The session that comes back is opened after that sweep rather than before it,
// which is the order the whole answer depends on: opening one first would have it
// deleted by the revocation that follows, and a link that set a password and then
// signed nobody in is the half-journey the front door was scored zero on — the
// invited person who chose a password in the page the mail opened and then had to
// sign in anyway. The method it records is "reset", not "password": what this
// person spent was a token, and the trail says what was spent.
//
// nil and no error is the one account this leaves signed out: a person who
// enrolled a second factor. Their password is still set and their other sessions
// still ended — a dead phone must not lock anybody out of their own account — but
// the first half of a sign-in does not open a session on an account that answers
// with two, and a mailed link is a weaker first half than a password. They are
// sent to the sign-in page to finish the sign-in the way the account asks for.
func (s *Service) Reset(ctx context.Context, tx db.Tx[db.Tenant], token, password string,
	from contracts.Client) (*contracts.Session, error) {
	if token == "" {
		return nil, contracts.ErrCredentials
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
		return nil, contracts.ErrCredentials
	case err != nil:
		return nil, fmt.Errorf("auth: consume a password token: %w", err)
	}
	if err := s.users.SetPassword(ctx, tx, userID, password); err != nil {
		return nil, err
	}
	if err := s.RevokeSessions(ctx, tx, userID, uuid.Nil); err != nil {
		return nil, err
	}
	if err := events.Publish(ctx, tx, contracts.EventPasswordReset, contracts.PasswordReset{
		UserID: userID, At: db.Now(),
	}); err != nil {
		return nil, err
	}
	answersWith, err := s.factorEnrolled(ctx, tx, userID)
	if err != nil {
		return nil, err
	}
	if answersWith {
		return nil, nil
	}
	person, err := s.users.Get(ctx, tx, userID)
	if err != nil {
		return nil, err
	}
	session, _, err := s.open(ctx, tx, person, from, "reset")
	return session, err
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

// ResetPath is where the link points, and it is the workspace address rather than
// a bare path: the page that reads the token lives under the workspace prefix the
// kernel composes (httpx.Workspace), in the namespace this module's screens are
// mounted at. The bare "/auth/reset" this constant used to carry was an address no
// route answered — the API sits at /api/v1/auth/password/reset and the shell served
// no page at all — so an invitation mail and a reset mail each carried a link that
// opened nothing, which is the academy finding in one line. The host in front of it
// is still the tenant's own, from the host of record, never a configured port.
var ResetPath = httpx.Workspace("/auth/reset")

// VerifyEmailPath is the confirmation link a sign-up mail carries, and it is the
// workspace screen the shell mounts for it — the same correction ResetPath got.
// The bare "/auth/verify-email" this used to be an address no route answered:
// the API door is /api/v1/public/auth/verify-email and the page sits under the
// workspace prefix, so a person who signed up was mailed a link to a 404 and
// could not activate the account they had just made.
var VerifyEmailPath = httpx.Workspace("/auth/verify-email")

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
