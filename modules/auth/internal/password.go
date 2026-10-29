package internal

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
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
//
// A composition that cannot deliver is refused with 503 and writes nothing.
func (s *Service) Forget(ctx context.Context, tx db.Tx[db.Tenant], email string) error {
	if !s.mail.deliversAccountMail() {
		return problem.New(http.StatusServiceUnavailable, "account email delivery is unavailable")
	}
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
	if s.invitationUsers != nil {
		return s.offerLegacyInvitation(ctx, tx, userID)
	}
	return s.offerUser(ctx, tx, userID)
}

func (s *Service) offerUser(ctx context.Context, tx db.Tx[db.Tenant], userID uuid.UUID) error {
	user, err := s.users.Get(ctx, tx, userID)
	if errors.Is(err, crud.ErrNotFound) {
		return nil
	}
	if err != nil || user.Status != usercontracts.StatusInvited {
		return err
	}
	return s.offer(ctx, tx, user, inviteSubject, inviteBody)
}

// offer mints the token and registers the mail on the commit that makes it
// spendable, except where the person may be holding a link, which is mailed first.
//
// # Where the row sits, relative to the mail
//
// An offer of a credential nobody holds yet commits before the message leaves, and
// what waits for the commit is the send and its outcome (see mailAfterCommit). A
// mailbox that shows a link with no committed row behind it is a link that does not
// work yet — apps/platformkit's four-eyes journey redeems the token the moment the
// mail appears — and a transaction that rolls back after handing the link over leaves
// a mail this application had forgotten minting. What that trade buys is stated where
// it is made: a transport that refuses the message after the commit leaves a row whose
// send never happened, and nothing here can un-commit it. The row goes back the way the
// attempt found it and the refusal is recorded (see restoreUnmailed), so a committed
// failure holds no live credential nobody was sent, and the withdraw names this
// attempt's own hash, so the cooldown is left counting the link that really left.
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
// So the notice raised here carries the set-password page and nothing else —
// the person has something to see in the application, telling them to check
// their mail — and the secret goes from this transaction straight to the mail
// server. The composition wires notification's Mailer, so there is one sender.
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
// passwordLock is taken before recent(), because recent() is a read: two attempts
// that both read "no link yet" would both mail. It is held to this transaction's
// commit, so nothing else rotates or spends this person's credential while this offer's
// write is in flight; what reaches past the commit is the mail slot, which the hand-off
// holds across the send and its marks (see holdMailSlot), and which an offer asks about
// before it mints. The two writes on the far side of the commit name this attempt's own
// hash rather than trusting either lock.
//
// sent_at is what makes the ordering survivable in both directions. It records the
// delivery the transport took, written in the same transaction as the record of the
// send, so a delivery that comes round again finds the cooldown honouring the link
// already in the mailbox and mails nobody a second one, and a row whose mail never
// reached the transport — withdrawn as refused, or left by a process that died
// before the send — holds nobody's cooldown down and is offered again.
//
// # The one offer this ordering cannot make
//
// A rotation of a link this person was already sent turns the rule over. There
// the row is not an offer of a credential but the credential somebody holds and
// may be opening while this delivery runs, and a write that precedes the send
// would destroy it on a transport that refuses the replacement: a dead link in
// the inbox, and no live one anywhere. That offer mails first and writes once
// the transport has taken the message — see rotate — which keeps one rule intact
// in both directions: the write that ends a delivered credential happens only
// when a replacement is on its way to the person who holds it.
func (s *Service) offer(ctx context.Context, tx db.Tx[db.Tenant], user *usercontracts.User, title, body string) error {
	if !contracts.MailDeliverable(s.mail.Mailer) {
		// A composition with no mailer writes no token either: a link nobody is
		// sent is a live credential in a table for an hour, for nothing. It leaves a
		// suppressed record, because the deployment rather than the caller is what
		// has to learn that no transport is wired.
		slog.WarnContext(ctx, "auth: no mailer is wired, so no set-password link was sent", "user", user.ID)
		return s.recordMail(ctx, tx, contracts.MailSetPassword, user.Email,
			notificationcontracts.MailSuppressed, "no mail transport is wired")
	}
	if err := passwordLock(tx, user.ID); err != nil {
		return err
	}
	recent, err := s.recent(tx, user.ID)
	if err != nil || recent {
		return err
	}
	// A link of this person's that is with the transport right now is the link this
	// person is being sent. The send leaves on the commit of the transaction that minted
	// it, so while it is in the air the table holds that row unmarked, and the cooldown
	// that counts sent marks cannot see it: minting over it would mail a second link of
	// which the single-pending write keeps only the last, and the first envelope — the
	// one already open in front of the person — would answer "those credentials are not
	// right", with the cooldown holding the next ask off behind it.
	if open, err := mailSlotOpen(tx, "password_tokens", user.ID); err != nil || !open {
		return err
	}
	// Whether this person holds a link that still works is the other half of what
	// the cooldown asked, and it decides where the replacement is written: see
	// rotate.
	held, err := s.held(tx, user.ID)
	if err != nil {
		return err
	}
	// The credential this person already holds, read under the same lock as the
	// write: a refused replacement has to put this row back rather than delete "the"
	// row, because it may be the only link they were sent.
	was, err := pendingLinkOf(tx, "password_tokens", user.ID)
	if err != nil {
		return err
	}
	// Read before anything is written: a tenant served at no host of record mails
	// nothing, and that has to be known while the offer is still nothing.
	base, err := s.baseURL(ctx, tx)
	if err != nil {
		return err
	}
	token, at := secret(), db.Now()
	if held {
		return s.rotate(ctx, tx, user, title, body, base, token, at)
	}
	if err := s.writeOffer(tx, user.ID, token, at, false); err != nil {
		return err
	}
	// What waits for the commit is the send and the marks of its outcome. Everything
	// the message needs — the recipient, the tenant's host, the row, the previous
	// link — was read above, in the transaction that makes the token spendable. The
	// notice is described here and raised on the other side of the commit, with the
	// send, because its copy tells the person the link is in their email and nothing
	// raises it until the transport has taken the message.
	link := base + s.mail.SetPasswordPage
	return events.Defer(ctx, func(ctx context.Context) error {
		return s.mailAfterCommit(ctx, linkMail{
			table: "password_tokens", kind: contracts.MailSetPassword,
			person: user.ID, address: user.Email, token: token,
			digest: contracts.Hash(token), was: was,
			msg:    mailMessage(user.Email, title, body, link, token),
			notice: setPasswordNotice(user.ID, title, body, s.mail.SetPasswordPage),
		})
	})
}

// ErrDeliveryRefused says the transport refused a credential this module minted
// for it. The record of the refusal is written in the caller's transaction, so an
// event worker that has to commit that record acknowledges this answer while the
// delivery itself still rolls back, and a caller that asked for the mail directly
// learns the mail did not go. It carries no words of its own: a transport may
// quote its input in an error, the outbox retains handler errors, and the input
// here is a link nobody holds.
var ErrDeliveryRefused = errors.New("auth: the mail transport refused the credential")

// AcknowledgeRefusedDelivery wraps a subscription handler with the answer a refused
// rotation deserves from an event worker: acknowledged. The record of a mail no
// transport took is written in the event's own transaction, so a handler that returns
// the failure rolls back the one artefact that says the mail did not go, and the
// person reading the delivery door is left to guess. The held credential itself is
// still where it was before the send, and the retry ladder a refusal would otherwise
// earn is four more attempts at one mailbox. A caller that asked for the mail in a
// command still gets the sentinel, and its transaction unwinds beside the delivery.
// An offer of a credential nobody holds answers differently, because its send waits
// for the commit: that refusal comes back as an owed effect (see mailAfterCommit).
func AcknowledgeRefusedDelivery(h events.Handler) events.Handler {
	return func(ctx context.Context, tx db.Tx[db.Tenant], ev events.Event) error {
		err := h(ctx, tx, ev)
		if errors.Is(err, ErrDeliveryRefused) {
			return nil
		}
		return err
	}
}

// refuseSetPassword records the refused send in the caller's transaction and
// answers with the sentinel. The transport's own words go to the record, redacted
// of the credential; they never travel in the error.
func (s *Service) refuseSetPassword(ctx context.Context, tx db.Tx[db.Tenant],
	email string, cause error, token string) error {
	if err := s.recordMail(ctx, tx, contracts.MailSetPassword, email,
		notificationcontracts.MailFailed, cause.Error(), token, string(contracts.Hash(token))); err != nil {
		return err
	}
	return ErrDeliveryRefused
}

// passwordLock serialises the two things that must not happen twice for one
// person: sending a second link inside the cap, and minting a token a concurrent
// attempt is about to replace. It is the same advisory lock verificationLock takes
// (verification.go), keyed the same way, and it is taken before recent() because
// recent() is a read: two attempts that both read "no link yet" would both mail.
// Postgres holds it to this transaction's commit and no further, which is the reach
// of every lock a transaction takes; the send leaves on that commit, so the offer that
// arrives while a message is still with the transport is kept off by the mail slot
// (see mailSlotOpen), not by this lock. The key includes the tenant even though table
// visibility is already RLS-bound.
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
	sent    sql.NullTime
	there   bool
}

// pendingLinkOf reads the one outstanding link a person holds. It is called under
// the advisory lock, and it is what a refused attempt puts back rather than deleting
// "the" row, because the row it replaced may be the only link that person has. sent_at
// comes back with the row, because that column is the record of which delivery the
// transport took.
func pendingLinkOf(tx db.Tx[db.Tenant], table string, id uuid.UUID) (pendingLink, error) {
	var out pendingLink
	err := tx.DB().Raw("SELECT token_hash, created_at, expires_at, sent_at FROM "+table+
		" WHERE tenant_id = ? AND user_id = ?", db.TenantOf(tx).ID, id).Row().
		Scan(&out.hash, &out.created, &out.expires, &out.sent)
	switch {
	case errors.Is(err, sql.ErrNoRows), errors.Is(err, gorm.ErrRecordNotFound):
		return out, nil
	case err != nil:
		return out, fmt.Errorf("auth: read the outstanding link in %s: %w", table, err)
	}
	out.there = true
	return out, nil
}

// offerStatement is the single-pending write, spelled once for the row that waits
// for its mail and for the row written once the transport took the message. Its %s
// is what each leaves sent_at: NULL for an offer that has not been mailed, which
// becomes a delivery in the mark written beside the send (see markLinkSent), and the
// clock for a rotation, whose send already happened. EXCLUDED carries the same value
// into the update branch, so a rotated link is as (un)delivered as a fresh one.
const offerStatement = "INSERT INTO password_tokens (token_hash, tenant_id, user_id, created_at, expires_at, sent_at)" +
	" VALUES (?, ?, ?, ?, ?, %s) ON CONFLICT (tenant_id, user_id) DO UPDATE SET" +
	" token_hash = EXCLUDED.token_hash, created_at = EXCLUDED.created_at," +
	" expires_at = EXCLUDED.expires_at, sent_at = EXCLUDED.sent_at"

// rotate replaces a link this person can be holding: the message goes first,
// and the credential it names is written only once the transport has taken it.
//
// This is offer's ordering reversed, for the one case where the row under the
// write is not an offer but somebody's working credential. Writing ahead of the
// send would make a refused transport the author of that person's bad hour: the
// unexpired link in their mailbox answers "those credentials are not right", the
// replacement never left, and the cooldown holds the next attempt off for five
// minutes more. Mailed first, a rotation supersedes the old link only once the
// replacement has arrived; a refusal writes nothing, old row and expiry intact.
//
// The window it accepts is the short one: between the transport's answer and this
// write the new mail names a link the table does not hold yet, and a person
// reading that fast finds the old link, which still works.
func (s *Service) rotate(ctx context.Context, tx db.Tx[db.Tenant], user *usercontracts.User,
	title, body, base, token string, at time.Time) error {
	if err := s.send(ctx, user, title, body, base, token); err != nil {
		// Nothing was written, so nothing needs withdrawing: the refusal is a record
		// and an answer, not a rollback.
		return s.refuseSetPassword(ctx, tx, user.Email, err, token)
	}
	if err := s.commitDelivered(ctx, tx, user.ID, token, at); err != nil {
		return err
	}
	// The transport took the replacement, so the record one accepted send is owed is
	// written here rather than on the far side of a commit: this offer raised no send
	// that waits, and a mail an operator's ledger cannot find is a mail nobody can
	// account for.
	if err := s.recordMail(ctx, tx, contracts.MailSetPassword, user.Email,
		notificationcontracts.MailSent, ""); err != nil {
		return err
	}
	if s.notify == nil {
		return nil
	}
	// The transport took the replacement, so the notice one accepted mail is owed is
	// raised here rather than across a commit: this offer raised no send that waits.
	_, err := s.notify.Notify(ctx, tx, setPasswordNotice(user.ID, title, body, s.mail.SetPasswordPage))
	return err
}

// setPasswordNotice is the in-app notice one accepted set-password mail is owed. It
// carries a path and nothing else: the notification module refuses an absolute link,
// and the credential travels in the mail, never in the notice.
func setPasswordNotice(userID uuid.UUID, title, body, page string) notificationcontracts.Notice {
	return notificationcontracts.Notice{
		Recipient: userID, Title: title,
		Body: body + "\n\nThe link is in the email this raised. It works once and stops working in an hour.",
		Link: page,
	}
}

// commitDelivered writes a credential whose email the transport already took, in
// the same write as the mark that says it went out. Row and delivery stand or fall
// together, which is what sent_at beside the insert describes (see offerStatement):
// an event that rolls back after an accepted send leaves the link a mailbox holds
// spendable, and honouring the cooldown that send earned.
//
// The write is its own transaction wherever the caller carries the application pool,
// because that is what lets it survive the roll back of the delivery that mailed it.
// A caller with no pool holds the send inside its own transaction and can hold the
// row there and no further.
func (s *Service) commitDelivered(ctx context.Context, tx db.Tx[db.Tenant], userID uuid.UUID,
	token string, at time.Time) error {
	if _, pooled := httpx.ConnFrom(ctx); !pooled {
		return s.writeOffer(tx, userID, token, at, true)
	}
	err := s.writeCredential(ctx, "commit", "set-password", func(ahead db.Tx[db.Tenant]) error {
		if err := ahead.DB().Exec(writeGrace).Error; err != nil {
			return err
		}
		return s.writeOffer(ahead, userID, token, at, true)
	})
	if err == nil || !isBlockedByOwnWrite(err) {
		return err
	}
	// The row this rotation would replace is still an uncommitted mint of the very
	// transaction asking: its own write would wait behind that transaction, and nobody
	// has ever been mailed from a row nobody committed. So the credential goes in beside
	// the send instead, where the two stand or fall together — the other shape this
	// module's one rule already has.
	return s.writeOffer(tx, userID, token, at, true)
}

// writeOffer writes this attempt's one pending set-password row, marked delivered
// or not as its ordering asks (see offerStatement). ON CONFLICT on
// (tenant_id, user_id) is the single-pending rule: asking again replaces the last
// link rather than adding a second, so a mailbox with four of these mails still has
// one that works.
func (s *Service) writeOffer(tx db.Tx[db.Tenant], userID uuid.UUID, token string,
	at time.Time, delivered bool) error {
	err := tx.DB().Exec(fmt.Sprintf(offerStatement, sentStamp(delivered)),
		contracts.Hash(token), db.TenantOf(tx).ID, userID, at, at.Add(contracts.TokenLifetime)).Error
	if err != nil {
		return fmt.Errorf("auth: issue a password token: %w", err)
	}
	return nil
}

// send hands the rotated link to the mail server. The link opens the page the
// composition named (Delivery.SetPasswordPage, the workspace screen when it names
// none), absolute and on the recipient's own tenant's host, because a mail client has
// no base to resolve a path against and one customer's people must not be sent to
// another's front door. The base is read by the caller, before anything is written.
func (s *Service) send(ctx context.Context, user *usercontracts.User, title, body, base, token string) error {
	return s.mail.Mailer.Send(ctx, mailMessage(user.Email, title, body, base+s.mail.SetPasswordPage, token))
}

// mailMessage renders the one message this module writes. page is the link without
// its token, absolute, because a mail client has no base to resolve a path against.
//
// The token is in this string and in nothing it returns to: the caller hands it
// straight to the mail server, and the record written beside the send names no
// subject, body, link or credential.
func mailMessage(to, title, body, page, token string) notificationcontracts.Message {
	return notificationcontracts.Message{
		To: to, Subject: title,
		Body: body + "\n\n" + page + "?token=" + token +
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

// held reports whether this person can be holding a link that still works.
//
// It is asked beside the cooldown and answers a different question from it: the
// cooldown counts links sent inside ResetInterval, so a link older than that has
// stopped limiting requests while it goes on working for another hour. The answer
// moves exactly one write — ahead of the send, or after it (see rotate) — and it
// is read off the same one row per person the offer itself writes.
//
// Expiry asks it, and no delivery bookkeeping does. sent_at records the send a
// transport took, which is the fact the cooldown needs and not the fact this one
// asks: a row can go without that mark and still have a recipient — a link
// delivered before the column existed answers NULL, and one whose send was
// interrupted between its commit and its handover answers NULL until its retry.
// Reading sent_at here would let that bookkeeping author the loss rotate exists to
// make impossible: the offer would write over a working link, the transport would
// refuse, and the withdrawal would leave the person with a dead link in the mail
// and no live one anywhere.
func (s *Service) held(tx db.Tx[db.Tenant], userID uuid.UUID) (bool, error) {
	var n int64
	err := tx.DB().Table("password_tokens").
		Where("user_id = ? AND expires_at > now()", userID).
		Count(&n).Error
	if err != nil {
		return false, fmt.Errorf("auth: read the delivered password token of %s: %w", userID, err)
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
	var err error
	if s.invitationUsers != nil {
		userID, err = s.consumePassword(ctx, tx, token, password)
	} else {
		err = tx.DB().Raw(
			"DELETE FROM password_tokens WHERE token_hash = ? AND expires_at > now() RETURNING user_id",
			contracts.Hash(token)).Row().Scan(&userID)
	}
	switch {
	case errors.Is(err, sql.ErrNoRows), errors.Is(err, gorm.ErrRecordNotFound):
		// An unknown token, a spent one and an expired one are one answer, for
		// the reason Login's three refusals are one answer.
		return nil, contracts.ErrCredentials
	case err != nil:
		return nil, fmt.Errorf("auth: consume a password token: %w", err)
	}
	if s.invitationUsers == nil {
		if err := s.users.SetPassword(ctx, tx, userID, password); err != nil {
			return nil, err
		}
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

// ResetPath is where the link points when the composition names no page of its
// own (Delivery.SetPasswordPage), and it is the workspace address rather than
// a bare path: the page that reads the token lives under the workspace prefix the
// kernel composes (httpx.Workspace), in the namespace this module's screens are
// mounted at. The bare "/auth/reset" this constant used to carry was an address no
// route answered — the API sits at /api/v1/auth/password/reset and the shell served
// no page at all — so an invitation mail and a reset mail each carried a link that
// opened nothing, which is the academy finding in one line. The host in front of it
// is still the tenant's own, from the host of record, never a configured port.
var ResetPath = httpx.Workspace("/auth/reset")

// VerifyEmailPath is the confirmation link a sign-up mail carries when the
// composition names none (Delivery.ConfirmEmailPage). It is the contracts
// constant, and unlike ResetPath this address is answered: the page sits on this
// module's public face, where a person with no session can reach it.
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
