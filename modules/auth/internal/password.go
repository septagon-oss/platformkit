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

// offer mints the token, raises the notice, and sends the mail.
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
// composition wires notification's own Mailer, so there is still one sender. It is
// handed the message after the commit rather than during it; see send.
//
// ON CONFLICT on (tenant_id, user_id) is the single-pending rule: asking again
// replaces the last link rather than adding a second, so a mailbox with four of
// these mails still has one that works. outstanding() is the other half of that —
// one link per person per ResetInterval, so an address somebody types
// repeatedly is one mail and not twenty — and the third half is the event id this
// INSERT writes, which keeps that cap from swallowing the retry of a mail that
// never went out.
func (s *Service) offer(ctx context.Context, tx db.Tx[db.Tenant], user *usercontracts.User, title, body string) error {
	if s.mail.Mailer == nil {
		// A composition with no mailer writes no token either: a link nobody is
		// sent is a live credential in a table for an hour, for nothing.
		slog.WarnContext(ctx, "auth: no mailer is wired, so no set-password link was sent", "user", user.ID)
		return nil
	}
	// Which delivery this mint belongs to, for the two questions outstanding()
	// asks about it. A link a request minted names no event, and NULL is what the
	// row says about that.
	ev, _ := events.DeliveryOf(ctx)
	var minted any
	if ev != uuid.Nil {
		minted = ev
	}
	capped, ours, err := s.outstanding(tx, user.ID, ev)
	if err != nil || capped {
		return err
	}
	token := secret()
	at := db.Now()
	err = tx.DB().Exec(
		"INSERT INTO password_tokens (token_hash, tenant_id, user_id, created_at, expires_at, event_id) VALUES (?, ?, ?, ?, ?, ?)"+
			" ON CONFLICT (tenant_id, user_id) DO UPDATE SET token_hash = EXCLUDED.token_hash,"+
			" created_at = EXCLUDED.created_at, expires_at = EXCLUDED.expires_at, event_id = EXCLUDED.event_id",
		contracts.Hash(token), db.TenantOf(tx).ID, user.ID, at, at.Add(contracts.TokenLifetime), minted).Error
	if err != nil {
		return fmt.Errorf("auth: issue a password token: %w", err)
	}
	if s.notify != nil && !ours {
		// A path, and one with no query on it: the notification module refuses
		// an absolute link, and this one is not a credential at all. Not raised when
		// this delivery minted the row it is retrying: the notice that went with the
		// link is already in the person's inbox inside the application, and the mail
		// is the only thing that failed. Two notices for one invitation is a person
		// told twice because a mail server hiccupped.
		_, err = s.notify.Notify(ctx, tx, notificationcontracts.Notice{
			Recipient: user.ID, Title: title,
			Body: body + "\n\nThe link is in the email this raised. It works once and stops working in an hour.",
			Link: ResetPath,
		})
		if err != nil {
			return err
		}
	}
	return s.send(ctx, tx, user, title, body, token)
}

// send renders the one message this module writes and defers its delivery to the
// commit of the transaction that minted the token. The link is absolute and on the
// recipient's own tenant's host, because a mail client has no base to resolve a
// path against and one customer's people must not be sent to another's front door.
//
// Everything that decides the message is read here, inside the transaction — the
// host of record, the interval check, the token row itself — and only the handover
// waits. A message handed over before the commit is a credential in an inbox that
// points at a row nobody can see yet: the recipient who follows the link the second
// it arrives is refused for a credential the installation really did issue, and a
// transaction that rolls back afterwards leaves a live-looking link in somebody's
// mailbox that this application has forgotten. Both of those are the recipient's
// problem and neither is ours to keep, so the mail leaves on the commit and not on
// the INSERT.
//
// What that costs, and what it no longer does: a mail server that refuses the
// message after the commit cannot be answered by a rollback that never happens, so
// the row is there and the mail is not. That delivery does not mark itself done. It
// releases its claim (kit/events), the outbox row stays unpublished, and the retry
// finds its own mint by the event id on it and sends again rather than reading it as
// a second ask. What ends unrecovered is a transport that stays down past the
// delivery's ladder: that failure then lands as a dead letter an operator reads and
// replays, the same shape every other undeliverable event in this application takes.
// The shape this replaced was a link anybody with the mail could try to spend before
// the transaction that authorised it existed.
func (s *Service) send(ctx context.Context, tx db.Tx[db.Tenant], user *usercontracts.User, title, body, token string) error {
	base, err := s.baseURL(ctx, tx)
	if err != nil {
		return err
	}
	msg := notificationcontracts.Message{
		To: user.Email, Subject: title,
		Body: body + "\n\n" + base + ResetPath + "?token=" + token +
			"\n\nThe link works once and stops working in an hour.",
	}
	// The action holds a rendered message and no handle: the transaction it waits
	// for is over by the time it runs, which is why base is read above.
	return db.AfterCommit(ctx, func(ctx context.Context) error { return s.mail.Mailer.Send(ctx, msg) })
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

// outstanding is the one read both halves of the cap need, off the one row this
// table keeps per person.
//
// capped says a link was minted for this person inside ResetInterval by some *other*
// ask. That is the cap on outstanding notices per recipient, and it is read off the
// token row rather than counted anywhere else because the token table is already one
// row per person: asking again replaces the link, so the only thing left to bound is
// how many mails and notices one ask produces. Without it, a public route plus a
// known address is somebody else's inbox filled by a stranger, one mail per request.
//
// ours says the row that is there is the one this delivery minted and still owes a
// mail to. It exists because the delivery that could not send leaves its row behind
// and comes back — its claim is released, not rolled back, because the commit that
// wrote the row is already somebody else's. Read without it, that row is a link
// minted seconds ago and capped, and one refused send becomes an invitation nobody is
// ever sent: the retry runs, finds "recent", sends nothing, and acks. A delivery
// names no event — a link minted by a request, or by any other work — and then ours
// is false and every recent link caps it, which is how the cap read before a retry
// could exist and how it still reads for anything that cannot be retried.
func (s *Service) outstanding(tx db.Tx[db.Tenant], userID, ev uuid.UUID) (capped, ours bool, err error) {
	err = tx.DB().Raw(
		"SELECT count(*) FILTER (WHERE created_at > now() - ?::interval AND (event_id IS NULL OR event_id <> ?)) > 0,"+
			" count(*) FILTER (WHERE event_id = ?) > 0"+
			" FROM password_tokens WHERE user_id = ?",
		resetInterval, ev, ev, userID).Row().Scan(&capped, &ours)
	if err != nil {
		return false, false, fmt.Errorf("auth: read the pending password token of %s: %w", userID, err)
	}
	return capped, ours, nil
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
