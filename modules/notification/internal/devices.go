package internal

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/push"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
)

// DeviceStore is contracts.DeviceService over this module's own device table,
// read and written in the caller's transaction — the same shape Prefs has, and
// for the same reason: there is nothing to hold, so a composition needs no
// invalidation and a test needs no fixture beyond the schema.
//
// Adapters is the one collaborator it cannot be without. Registration is the
// moment a token can be checked for carryability, and only the carrier of a
// platform can say whether it could carry one: a store that wrote a token no
// adapter could send to would fail every later notice to that person with an
// error no retry changes. An empty map is a deployment that carries no push at
// all, and it refuses every registration rather than storing an address it will
// never reach.
type DeviceStore struct {
	Adapters push.Adapters
}

var _ contracts.DeviceService = DeviceStore{}

// owning is choosing's rule for the device rows: row-level security answers
// "whose tenant", which is not the question a device asks, because two people in
// one tenant are two people and one of them registering a token at the other's
// name is the same harm with a tenant-shaped excuse. A caller with no principal
// is not anonymous — it is platform work that named the person it is writing for
// — and a principal that is not that person is refused before anything is read.
func owning(ctx context.Context, recipient uuid.UUID) error {
	if principal, ok := tenancy.PrincipalFrom(ctx); ok && principal.UserID != recipient {
		return fmt.Errorf("%w: notification: a person's registered devices are their own, and this caller is somebody else",
			tenancy.ErrPolicyDenied)
	}
	return nil
}

// RegisterDevice writes the caller's device, keyed on the live (tenant, platform,
// token) triple rather than on an id the client cannot know.
//
// Twice in a row from the same person is one row and no event: the second call
// takes the found-and-mine branch, which writes nothing, so a client that retried
// a registration does not leave two rows and two audit entries for one phone.
// The same token from another person of the tenant moves it, because whoever
// holds the token is whoever can be pushed to — the previous owner's notices must
// stop arriving, and the row that stays would keep sending them.
//
// The row is read under its write lock first, so two concurrent registrations of
// one token serialise and the loser takes the found branch rather than both
// answering "no row yet"; the partial unique index is the backstop that makes a
// lost insert a conflict this command re-reads instead of a duplicate.
func (s DeviceStore) RegisterDevice(ctx context.Context, tx db.Tx[db.Tenant], recipient uuid.UUID, in contracts.DeviceInput) (*contracts.Device, error) {
	if recipient == uuid.Nil {
		return nil, fmt.Errorf("%w: notification: a device belongs to the person registering it, and this call names nobody", crud.ErrInvalid)
	}
	if err := owning(ctx, recipient); err != nil {
		return nil, err
	}
	if err := s.carries(in.Platform); err != nil {
		return nil, err
	}
	row, err := s.live(tx, in.Platform, in.Token)
	if err != nil {
		return nil, err
	}
	if row != nil {
		return s.settle(ctx, tx, row, recipient)
	}
	row = &contracts.Device{RecipientID: recipient, Platform: in.Platform, Token: in.Token}
	if err := crud.Create(ctx, tx, row); err != nil {
		if !errors.Is(err, crud.ErrConflict) {
			return nil, err
		}
		// Somebody else wrote this token between the read and the insert. Their
		// row is the answer either way, so it is read back and settled the same
		// way a found row is — and if it still cannot be read, the conflict is
		// returned rather than swallowed.
		// Somebody else wrote this token between the read and the insert. Their
		// row is the answer either way, so it is read back and settled the same
		// way a found row is — and a conflict with no row to read back is returned
		// rather than swallowed.
		live, again := s.live(tx, in.Platform, in.Token)
		if again != nil {
			return nil, again
		}
		if live == nil {
			return nil, err
		}
		return s.settle(ctx, tx, live, recipient)
	}
	return row, events.Publish(ctx, tx, contracts.EventDeviceRegistered, contracts.DeviceRegistered{
		DeviceID: row.ID, Recipient: recipient, Platform: row.Platform, At: db.Now(),
	})
}

// carries is the deployment's answer about a platform, asked of the adapters this
// installation was built with: an unknown spelling and a platform it carries no
// adapter for are both refused before a row exists, because a device nobody can
// send to is a row that fails every notice to that person and a token a client
// believes is registered.
func (s DeviceStore) carries(platform push.Platform) error {
	if err := platform.Validate(); err != nil {
		return fmt.Errorf("%w: %w", crud.ErrInvalid, err)
	}
	if s.Adapters.Get(platform) == nil {
		return fmt.Errorf("%w: notification: this installation carries no %s push, so nothing could ever be sent to that device",
			crud.ErrInvalid, platform)
	}
	return nil
}

// settle is the answer for a token that is already a live device: the caller's
// own changes nothing and says nothing, and another person's moves to the caller
// with one event saying so.
func (s DeviceStore) settle(ctx context.Context, tx db.Tx[db.Tenant], row *contracts.Device, recipient uuid.UUID) (*contracts.Device, error) {
	if row.RecipientID == recipient {
		return row, nil
	}
	row.RecipientID = recipient
	if err := crud.Update(ctx, tx, row, "recipient_id", "updated_at"); err != nil {
		return nil, err
	}
	return row, events.Publish(ctx, tx, contracts.EventDeviceRegistered, contracts.DeviceRegistered{
		DeviceID: row.ID, Recipient: recipient, Platform: row.Platform, At: db.Now(),
	})
}

// ListDevices is the caller's own devices. The recipient filter is set here
// rather than taken from the query, so no shape of Query lists anybody else's,
// and the caller's principal is checked the way the writes check it — the rows
// are the person's, and row-level security cannot say so inside one tenant.
func (s DeviceStore) ListDevices(ctx context.Context, tx db.Tx[db.Tenant], recipient uuid.UUID, q crud.Query) ([]*contracts.Device, int64, error) {
	if err := owning(ctx, recipient); err != nil {
		return nil, 0, err
	}
	q.Filter = map[string]any{"recipientId": recipient}
	if q.Sort == "" {
		q.Sort = "-createdAt"
	}
	return crud.List[*contracts.Device](tx, q)
}

// RemoveDevice is the person's own deletion of one of their devices: the row is
// soft-deleted and the event says who did it. An id that is not the caller's live
// device — never existed, already gone, or somebody else's — is ErrNotFound, so
// the route stays SignedIn and a probe learns nothing about anybody else's phone.
func (s DeviceStore) RemoveDevice(ctx context.Context, tx db.Tx[db.Tenant], id, recipient uuid.UUID) error {
	if err := owning(ctx, recipient); err != nil {
		return err
	}
	row, err := crud.Get[*contracts.Device](tx, id)
	if err != nil {
		return err
	}
	if row.RecipientID != recipient {
		return crud.ErrNotFound
	}
	if err := crud.Delete[*contracts.Device](tx, id, true); err != nil {
		return err
	}
	return events.Publish(ctx, tx, contracts.EventDeviceRemoved, contracts.DeviceRemoved{
		DeviceID: row.ID, Recipient: recipient, Platform: row.Platform,
		Reason: "removed by the person", Actor: callerOf(ctx), At: db.Now(),
	})
}

// forgetGone is the other way a device row ends: the provider said this token is
// not a device any more, so the row that holds it is a lie about a thing that is
// not there. It runs in the delivery's own transaction, so the deletion, its
// event and the ledger row the send closes commit together or not at all — a
// gone row that outlives a rolled-back delivery would be a device deleted by a
// send that never happened.
//
// The actor is uuid.Nil on purpose: no person removed this, the far end did.
func forgetGone(ctx context.Context, tx db.Tx[db.Tenant], row *contracts.Device, reason string) error {
	if err := crud.Delete[*contracts.Device](tx, row.ID, true); err != nil {
		return err
	}
	return events.Publish(ctx, tx, contracts.EventDeviceRemoved, contracts.DeviceRemoved{
		DeviceID: row.ID, Recipient: row.RecipientID, Platform: row.Platform,
		Reason: reason, Actor: uuid.Nil, At: db.Now(),
	})
}

// live is the one live device holding this token, with its write lock held, or
// nil. It is the whole of the identity question, so it is the one query that
// reads the unique index.
func (DeviceStore) live(tx db.Tx[db.Tenant], platform push.Platform, token string) (*contracts.Device, error) {
	var row contracts.Device
	err := tx.DB().Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("platform = ? AND token = ? AND deleted_at IS NULL", string(platform), token).
		Take(&row).Error
	switch {
	case errors.Is(err, nil):
		return &row, nil
	case errors.Is(err, gorm.ErrRecordNotFound):
		return nil, nil
	default:
		return nil, crud.Classify(err)
	}
}

// devicesOf is a person's live devices this channel can carry, in the one query
// the carrier makes. No paging: a person holds a handful of devices, and a
// delivery that carried some of them and skipped the rest because of a page size
// would be a notice that arrives at random.
func devicesOf(tx db.Tx[db.Tenant], recipient uuid.UUID, channel contracts.Channel) ([]*contracts.Device, error) {
	var out []*contracts.Device
	err := tx.DB().Where("recipient_id = ? AND deleted_at IS NULL", recipient).
		Order("created_at DESC").Find(&out).Error
	if err != nil {
		return nil, crud.Classify(err)
	}
	reachable := out[:0]
	for _, d := range out {
		if carries(channel, d.Platform) {
			reachable = append(reachable, d)
		}
	}
	return reachable, nil
}

// callerOf is whose credential this request carries, or uuid.Nil when none does.
// The device events name it so that a person's own deletion and a provider's
// report of a gone token are told apart in the audit trail.
func callerOf(ctx context.Context) uuid.UUID {
	if principal, ok := tenancy.PrincipalFrom(ctx); ok {
		return principal.UserID
	}
	return uuid.Nil
}
