// Package internal is every implementation of the notification module. Nothing
// outside modules/notification can import it, which is the compiler enforcing
// idea 3.
package internal

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
)

// Service writes notifications, decides what a notice may be told by, and reads
// one person's back. Its collaborators are the three lookups the decision needs
// — an address, the person's own settings, the tenant's sender — and the
// carriers this deployment has, and all of them are optional in the way that a
// deployment that says nothing about push sends no push.
type Service struct {
	recipients contracts.RecipientLookup
	prefs      contracts.Preferences
	senders    contracts.Senders
	providers  contracts.Providers
	clock      func() time.Time
}

// Option is one collaborator the composition has. NewService takes them as
// options rather than as arguments because every one of them has an honest
// default, and a positional list of five mostly-nil arguments is how a wiring
// mistake becomes a nil dereference in a worker an hour later.
type Option func(*Service)

// WithPreferences wires the person's own answers: the channel switches, the
// opt-out and the quiet window. Without it no person has chosen anything, which
// is what a deployment without a settings screen means.
func WithPreferences(p contracts.Preferences) Option { return func(s *Service) { s.prefs = p } }

// WithSenders wires the tenant's own sender. Without it the installation speaks
// as its own configured address for every tenant — the behaviour this module
// shipped with — and Decide is told so by Scope.SenderOptional rather than being
// handed a sender it did not ask for.
func WithSenders(s contracts.Senders) Option { return func(s2 *Service) { s2.senders = s } }

// WithProviders wires the carriers beyond mail. A channel with no carrier is not
// broken: Decide suppresses it with a reason and names the deployment as the one
// that could change it, which is why the ledger still accounts for it.
func WithProviders(p contracts.Providers) Option {
	return func(s *Service) {
		for c, pr := range p {
			s.providers[c] = pr
		}
	}
}

// WithClock is the clock the quiet-hour decision reads. A composition leaves it
// alone; a test that wants to know what happens at 22:00 in Lisbon in September
// passes one, because the answer is different in March and a test that cannot
// say which month it is in proves nothing.
func WithClock(c func() time.Time) Option { return func(s *Service) { s.clock = c } }

// NewService returns the service. module.go constructs it.
func NewService(recipients contracts.RecipientLookup, opts ...Option) *Service {
	s := &Service{recipients: recipients, providers: contracts.Providers{}, clock: db.Now}
	for _, o := range opts {
		o(s)
	}
	if s.clock == nil {
		s.clock = db.Now
	}
	return s
}

var _ contracts.Service = (*Service)(nil)

// Notify writes the row, asks Decide which channels may carry it, and accounts
// for every channel the notice asked for in the same transaction: the chosen
// ones open with a requested row and their event, the refused ones close at once
// with a suppressed row that says why and who could change it. A notice that
// asks for three channels and gets one sent and two refused is a notice nobody
// has to explain from logs.
//
// A recipient the lookup cannot find, or one with no address, is not an error,
// because refusing the whole call would mean somebody without an email address
// could not be told anything.
func (s *Service) Notify(ctx context.Context, tx db.Tx[db.Tenant], n contracts.Notice) (*contracts.Notification, error) {
	row := &contracts.Notification{
		RecipientID: n.Recipient, Title: n.Title, Body: n.Body, Link: n.Link,
	}
	if err := crud.Create(ctx, tx, row); err != nil {
		return nil, err
	}
	at := db.Now()
	err := events.Publish(ctx, tx, contracts.EventCreated, contracts.Created{
		NotificationID: row.ID, Recipient: row.RecipientID, Title: row.Title, At: at,
	})
	if err != nil {
		return nil, err
	}
	decision, err := s.decide(ctx, tx, n)
	if err != nil {
		return row, err
	}
	for _, c := range decision.Chosen {
		if err := s.chosen(ctx, tx, row, n, c, at); err != nil {
			return row, err
		}
	}
	for _, sup := range decision.Suppressed {
		if err := record(tx, row.ID, string(sup.Channel), OutcomeRequested, ""); err != nil {
			return row, err
		}
		if err := record(tx, row.ID, string(sup.Channel), OutcomeSuppressed, sup.Reason); err != nil {
			return row, err
		}
	}
	return row, nil
}

// decide runs the one channel decision, over the reads it belongs to: the same
// transaction that writes the row is the one that reads what the person chose and
// what the tenant may send as, so a preference two requests apart cannot decide
// this notice. contracts.Decide holds the rules; this method only fetches what
// they need and says which deployment this is.
func (s *Service) decide(ctx context.Context, tx db.Tx[db.Tenant], n contracts.Notice) (contracts.Decision, error) {
	var (
		prefs  []contracts.Preference
		quiet  *contracts.QuietHours
		sender *contracts.Sender
		err    error
	)
	if s.prefs != nil {
		if prefs, err = s.prefs.Settings(ctx, tx, n.Recipient, n.Intent); err != nil {
			return contracts.Decision{}, err
		}
		if quiet, err = s.prefs.Quiet(ctx, tx, n.Recipient); err != nil {
			return contracts.Decision{}, err
		}
	}
	if s.senders != nil {
		if sender, err = s.senders.For(ctx, tx); err != nil {
			return contracts.Decision{}, err
		}
	}
	// Mail is carried by this module's own worker (internal.SendMail), which
	// module.go refuses to compose without a Mailer, so mail is always a channel
	// this deployment sends; every other channel is here because somebody wired a
	// provider for it.
	available := []contracts.Channel{contracts.ChannelEmail}
	for c := range s.providers {
		available = append(available, c)
	}
	slices.Sort(available)
	return contracts.Decide(n.Wants, n.Class, n.Intent, prefs, quiet, sender, contracts.Scope{
		Now: s.clock(), Available: available,
		SenderOptional: s.senders == nil,
	}), nil
}

// chosen opens one channel the decision allowed: in-app is the row itself, so it
// is requested and delivered by the write above; every other channel opens with a
// requested row and its own event, which the worker finishes in its own
// transaction. Mail is asked one question first — does this person have an
// address — because that is answerable here and nothing else is.
func (s *Service) chosen(ctx context.Context, tx db.Tx[db.Tenant], row *contracts.Notification, n contracts.Notice, c contracts.Channel, at time.Time) error {
	if err := record(tx, row.ID, string(c), OutcomeRequested, ""); err != nil {
		return err
	}
	if c == contracts.ChannelInApp {
		// The in-app channel is the row itself: written is delivered, so it is
		// requested and sent in the one transaction that wrote it.
		return record(tx, row.ID, ChannelInApp, OutcomeSent, "")
	}
	if c == contracts.ChannelEmail {
		to, err := address(ctx, tx, s.recipients, row.RecipientID)
		if err != nil {
			return err
		}
		if to == "" {
			return record(tx, row.ID, ChannelEmail, OutcomeSuppressed, "the recipient has no email address")
		}
	}
	name, ok := contracts.RequestedEvent[c]
	if !ok {
		return fmt.Errorf("notification: no event asks a worker for %s", c)
	}
	payload := any(contracts.DeliveryRequested{NotificationID: row.ID, Recipient: row.RecipientID, At: at})
	if c == contracts.ChannelEmail {
		// Mail keeps the payload it shipped with: an installation's own consumers
		// subscribe to the name (see contracts.EmailRequested).
		payload = contracts.EmailRequested{NotificationID: row.ID, Recipient: row.RecipientID, At: at}
	}
	return events.Publish(ctx, tx, name, payload)
}

// address is the recipient's email, or "" when there is nobody to ask or
// nothing to send to. A composition that wired no lookup is the second case:
// saying so beats a nil dereference, and the row is written either way.
//
// It is a function rather than a method because the worker asks the same
// question, in its own transaction, when the mail is actually sent.
func address(ctx context.Context, tx db.Tx[db.Tenant], recipients contracts.RecipientLookup, recipient uuid.UUID) (string, error) {
	if recipients == nil {
		return "", nil
	}
	to, err := recipients.Email(ctx, tx, recipient)
	if errors.Is(err, crud.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("notification: find the address of %s: %w", recipient, err)
	}
	return to, nil
}

// MarkRead records that the recipient has seen it. A row belonging to somebody
// else is ErrNotFound rather than a refusal, which is what lets the route be
// SignedIn: any caller may ask, and the only thing they learn about somebody
// else's notification is that they do not have one with that id. Marking it
// read again changes nothing and publishes nothing.
func (s *Service) MarkRead(ctx context.Context, tx db.Tx[db.Tenant], id, recipient uuid.UUID) (*contracts.Notification, error) {
	row, err := crud.Get[*contracts.Notification](tx, id)
	if err != nil {
		return nil, err
	}
	if row.RecipientID != recipient {
		return nil, crud.ErrNotFound
	}
	if row.ReadAt != nil {
		return row, nil
	}
	at := db.Now()
	row.ReadAt = &at
	// The columns this changed, and no others: a whole-row write would put
	// every field back to what this transaction read.
	if err := crud.Update(ctx, tx, row, "read_at", "updated_at"); err != nil {
		return nil, err
	}
	return row, events.Publish(ctx, tx, contracts.EventRead, contracts.Read{
		NotificationID: row.ID, Recipient: recipient, At: at,
	})
}

// ListFor is a page of one person's notifications. The recipient filter is set
// here rather than taken from the caller's query, so there is no shape of Query
// that lists somebody else's rows; beyond that it is an ordinary crud.List,
// which is what gives it the paging, ordering and field checking every other
// list in the application has.
func (s *Service) ListFor(_ context.Context, tx db.Tx[db.Tenant], recipient uuid.UUID, q crud.Query) ([]*contracts.Notification, int64, error) {
	q.Filter = map[string]any{"recipientId": recipient}
	return crud.List[*contracts.Notification](tx, q)
}
