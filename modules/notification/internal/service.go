// Package internal is every implementation of the notification module. Nothing
// outside modules/notification can import it, which is the compiler enforcing
// idea 3.
package internal

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
)

// Service writes notifications and reads one person's back. Its one field is
// how it turns a recipient into an address, which the application supplies.
type Service struct{ recipients contracts.RecipientLookup }

// NewService returns the service. module.go constructs it.
func NewService(recipients contracts.RecipientLookup) *Service {
	return &Service{recipients: recipients}
}

var _ contracts.Service = (*Service)(nil)

// Notify writes the row and says so. When the notice asks for mail and the
// recipient has an address it publishes the request rather than sending
// anything (see contracts.EventEmailRequested); a recipient the lookup cannot
// find, or one with no address, is not an error, because refusing the whole
// call would mean somebody without an email address could not be told
// anything.
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
	if !n.Email {
		return row, nil
	}
	to, err := s.address(ctx, tx, row.RecipientID)
	if err != nil || to == "" {
		return row, err
	}
	return row, events.Publish(ctx, tx, contracts.EventEmailRequested, contracts.EmailRequested{
		NotificationID: row.ID, Recipient: row.RecipientID, To: to,
		Title: row.Title, Body: row.Body, Link: row.Link, At: at,
	})
}

// address is the recipient's email, or "" when there is nobody to ask or
// nothing to send to. A composition that wired no lookup is the second case:
// saying so beats a nil dereference, and the row is written either way.
func (s *Service) address(ctx context.Context, tx db.Tx[db.Tenant], recipient uuid.UUID) (string, error) {
	if s.recipients == nil {
		return "", nil
	}
	to, err := s.recipients.Email(ctx, tx, recipient)
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
