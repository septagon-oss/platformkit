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
	"github.com/jackc/pgx/v5/pgconn"
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
// composition wires notification's own Mailer, so there is still one sender.
//
// ON CONFLICT on (tenant_id, user_id) is the single-pending rule: asking again
// replaces the last link rather than adding a second, so a mailbox with four of
// these mails still has one that works. recent() is the other half of that —
// one link per person per ResetInterval, so an address somebody types
// repeatedly is one mail and not twenty.
//
// # Why the row is committed before the message leaves
//
// The credential is written in a transaction that ends here and the mail is
// handed over afterwards, because a link mailed from inside an open transaction
// has two answers a person can get from it, and neither is theirs to receive.
// Open the link while the delivery still holds that transaction open and the
// token is not yet a row anybody can read: the invitation arrives at an inbox
// whose link "does not work". Or have that transaction roll back after the send:
// the mailbox now holds a dead link, and the retry that follows it puts a live
// one underneath, so the first mail a person reads is the one that leads
// nowhere. Both were seen on a loaded runner, where a delivery twenty seconds
// from its commit is ordinary — and the shape is modules/auth/000050's, taken
// from the confirmation link this module mails the same way for the same reason.
//
// passwordOfferLock is held across the commit and the send, so nothing else
// rotates or spends this person's credential in between, and sent_at is what
// makes the ordering survivable in both directions: it records the delivery the
// transport took, in a write of its own, so a retry of a delivery that failed
// afterwards does not mail a second link over the one already in a mailbox, and
// a row whose mail never reached the transport — this function retracts it, or
// the process died before the send — holds nobody's cooldown down and is offered
// again.
func (s *Service) offer(ctx context.Context, tx db.Tx[db.Tenant], user *usercontracts.User, title, body string) error {
	if !contracts.MailDeliverable(s.mail.Mailer) {
		// A composition with no mailer writes no token either: a link nobody is
		// sent is a live credential in a table for an hour, for nothing.
		slog.WarnContext(ctx, "auth: no mailer is wired, so no set-password link was sent", "user", user.ID)
		return nil
	}
	if err := passwordOfferLock(tx, user.ID); err != nil {
		return err
	}
	recent, err := s.recent(tx, user.ID)
	if err != nil || recent {
		return err
	}
	// Read before anything is written: a tenant served at no host of record mails
	// nothing, and that has to be known while the offer is still nothing.
	base, err := s.baseURL(ctx, tx)
	if err != nil {
		return err
	}
	if s.notify != nil {
		// A path, and one with no query on it: the notification module refuses
		// an absolute link, and this one is not a credential at all. It stays in
		// the delivery's transaction: it carries no secret, and a notice raised by
		// an offer whose mail the transport refused must not outlive that offer.
		if _, err := s.notify.Notify(ctx, tx, notificationcontracts.Notice{
			Recipient: user.ID, Title: title,
			Body: body + "\n\nThe link is in the email this raised. It works once and stops working in an hour.",
			Link: ResetPath,
		}); err != nil {
			return err
		}
	}
	token, at := secret(), db.Now()
	ahead, err := s.postOffer(ctx, tx, user.ID, token, at)
	if err != nil {
		return err
	}
	if err := s.send(ctx, user, title, body, base, token); err != nil {
		// A transport may quote its input in an error, and the outbox retains
		// handler errors, so never carry the mailer's potentially
		// credential-bearing text. The credential goes with the failure: it is a
		// link nobody holds. When the row is the caller's own, its rollback —
		// which this error causes — takes the row already.
		if ahead {
			s.retractOffer(ctx, user.ID, token)
		}
		return errors.New("auth: set-password email delivery failed")
	}
	if !ahead {
		// The row was written with its sent_at beside it, because that shape
		// cannot put the send between two commits: row and delivery stand
		// together or roll back together.
		return nil
	}
	// The transport took the message, so the row stops being an offer and becomes
	// a delivery. Its own write, not the delivery's: the fact the cooldown reads
	// has to outlive an event transaction that rolls back afterwards, or the retry
	// mails a second link and the first one — the one already in the mailbox —
	// leads nowhere.
	return s.markOfferSent(ctx, user.ID, token)
}

// passwordOfferLock holds one person's set-password offer across the commit of
// the credential and the send that carries it, so that two offers of one address
// — an invitation and a forgotten-password request arriving together — cannot
// each mail a link of which the table keeps only the last.
func passwordOfferLock(tx db.Tx[db.Tenant], userID uuid.UUID) error {
	key := "auth/password-offer/" + db.TenantOf(tx).ID.String() + "/" + userID.String()
	if err := tx.DB().Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", key).Error; err != nil {
		return fmt.Errorf("auth: lock the set-password offer: %w", err)
	}
	return nil
}

// offerStatement is the single-pending write, spelled once for the transaction of
// its own and for the caller's. Its %s is what each leaves sent_at: NULL for the
// row that is about to be mailed — it becomes a delivery in the write that follows
// the transport — and the clock for the row that is mailed beside it, which cannot
// outlive that send. EXCLUDED carries the same value into the update branch, so a
// rotated link is as (un)delivered as a fresh one.
const offerStatement = "INSERT INTO password_tokens (token_hash, tenant_id, user_id, created_at, expires_at, sent_at)" +
	" VALUES (?, ?, ?, ?, ?, %s) ON CONFLICT (tenant_id, user_id) DO UPDATE SET" +
	" token_hash = EXCLUDED.token_hash, created_at = EXCLUDED.created_at," +
	" expires_at = EXCLUDED.expires_at, sent_at = EXCLUDED.sent_at"

// postOffer puts one set-password credential where the person reading the mail
// will look for it, and says whether it got a commit of its own.
//
// That commit is the rule, and the database is what decides whether it is
// possible: password_tokens.user_id is a foreign key, so the write itself answers
// whether the person the link names is a fact outside this transaction. A worker
// always finds that fact — the event that asked for the link is relayed from
// committed rows, so the invitation committed before the mail was ever offered —
// and a credential committed by a delivery that then fails is a live link in
// nobody's mail, which is the half-journey this ordering exists to end.
//
// One caller is refused that answer: the registration subscriber that invites an
// address and mails its link in the same breath (see RegistrationSubscription).
// There the row goes inside the caller's own transaction, where it and the send
// can only stand or fall together, which is what sent_at beside the insert says:
// a row that survives is a row whose mail went.
func (s *Service) postOffer(ctx context.Context, tx db.Tx[db.Tenant], userID uuid.UUID,
	token string, at time.Time) (bool, error) {
	expiry := at.Add(contracts.TokenLifetime)
	if _, pooled := httpx.ConnFrom(ctx); pooled {
		err := s.writeCredential(ctx, "commit", "set-password", func(ahead db.Tx[db.Tenant]) error {
			// Asked of the person before the row is written, because the read of a
			// row another transaction is still writing waits, while its absence
			// reads in one hop: an offer that cannot commit ahead must say so now
			// rather than wait two seconds to be told by the foreign key.
			var seen bool
			if err := ahead.DB().Raw("SELECT EXISTS (SELECT 1 FROM users WHERE id = ?)", userID).Row().Scan(&seen); err != nil {
				return err
			}
			if !seen {
				return errPersonNotCommitted
			}
			return ahead.DB().Exec(fmt.Sprintf(offerStatement, "NULL"),
				contracts.Hash(token), db.TenantOf(ahead).ID, userID, at, expiry).Error
		})
		if err == nil {
			return true, nil
		}
		if !errors.Is(err, errPersonNotCommitted) && !isUncommittedPerson(err) {
			return false, err
		}
	}
	return false, s.writeOfferIn(tx, userID, token, at)
}

// writeOfferIn is the credential as a write of the caller's transaction.
func (s *Service) writeOfferIn(tx db.Tx[db.Tenant], userID uuid.UUID, token string, at time.Time) error {
	err := tx.DB().Exec(fmt.Sprintf(offerStatement, "clock_timestamp()"),
		contracts.Hash(token), db.TenantOf(tx).ID, userID, at, at.Add(contracts.TokenLifetime)).Error
	if err != nil {
		return fmt.Errorf("auth: issue a password token: %w", err)
	}
	return nil
}

// errPersonNotCommitted is the ahead-commit's own read saying that the person this
// credential names is a row only the caller's transaction can see. It names an
// ordering rather than a database failure, which is why it is a fact of this
// module's and not an error string from the driver.
var errPersonNotCommitted = errors.New("auth: the person this credential names is not committed yet")

// isUncommittedPerson is Postgres saying the same thing the read above looks for,
// one instant later: the foreign key refusing a credential that names a person who
// stopped existing, or whose transaction committed between the read and the write.
func isUncommittedPerson(err error) bool {
	pg, isPostgres := errors.AsType[*pgconn.PgError](err)
	return isPostgres && pg.Code == "23503"
}

// retractOffer withdraws a credential whose email the transport refused, in a
// write of its own that commits while the failed delivery rolls back. Without it
// the row would sit for an hour as a credential nobody holds, with nothing in the
// table to say it was never mailed; the retry rotates what it finds and sends a
// fresh link either way.
func (s *Service) retractOffer(ctx context.Context, userID uuid.UUID, token string) {
	err := s.writeCredential(ctx, "withdraw", "set-password", func(tx db.Tx[db.Tenant]) error {
		return tx.DB().Exec("DELETE FROM password_tokens WHERE user_id = ? AND token_hash = ?",
			userID, contracts.Hash(token)).Error
	})
	if err != nil {
		slog.ErrorContext(ctx, "auth: could not withdraw a set-password credential its email could not reach",
			"user", userID, "error", err)
	}
}

// markOfferSent records, in a write that commits by itself, that the transport
// took this link. See offer: the cooldown must survive the event that carried the
// offer, rather than commit and roll back with it.
func (s *Service) markOfferSent(ctx context.Context, userID uuid.UUID, token string) error {
	return s.writeCredential(ctx, "record", "set-password", func(tx db.Tx[db.Tenant]) error {
		return tx.DB().Exec("UPDATE password_tokens SET sent_at = clock_timestamp()"+
			" WHERE user_id = ? AND token_hash = ?", userID, contracts.Hash(token)).Error
	})
}

// send renders the one message this module writes and hands it to the mail
// server. The link is absolute and on the recipient's own tenant's host,
// because a mail client has no base to resolve a path against and one
// customer's people must not be sent to another's front door. The base is read
// by the caller, ahead of the commit: see offer.
func (s *Service) send(ctx context.Context, user *usercontracts.User, title, body, base, token string) error {
	return s.mail.Mailer.Send(ctx, notificationcontracts.Message{
		To: user.Email, Subject: title,
		Body: body + "\n\n" + base + ResetPath + "?token=" + token +
			"\n\nThe link works once and stops working in an hour.",
	})
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
// token table rather than counted anywhere else because that table is already
// one row per person: asking again replaces the link, so the only thing left to
// bound is how many mails and how many notices that produces. Without it, a
// public route plus a known address is somebody else's inbox filled by a
// stranger, one mail per request.
//
// It counts sent_at — the links that went out — and not created_at, the rows
// that were written: an offer whose mail never reached the transport is not a
// recent delivery, and must not hold the cooldown down while nobody holds a link.
func (s *Service) recent(tx db.Tx[db.Tenant], userID uuid.UUID) (bool, error) {
	var n int64
	err := tx.DB().Table("password_tokens").
		Where("user_id = ? AND sent_at IS NOT NULL AND sent_at > now() - ?::interval", userID, resetInterval).
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

// VerifyEmailPath is the confirmation link a sign-up mail carries. It is the
// contracts constant, not a workspace screen: unlike ResetPath, this address is
// answered, by the page this module mounts on its public face, and a person who
// follows the link has no session to open the workspace with.
// modules/auth/internal's TestTheModuleAnswersWithExactlyTheSurfaceItDeclares
// pins the address on the module's own surface, and apps/platformkit's
// TestTheEmailedLinkOpensAPageThatConfirms asks the running application the same
// question end to end; the page's mount guard refuses a composition that parts
// them.
var VerifyEmailPath = contracts.VerifyEmailPath

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
