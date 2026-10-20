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
	if err := s.carries(in); err != nil {
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
	if err := s.insert(ctx, tx, row); err != nil {
		if !errors.Is(err, crud.ErrConflict) {
			return nil, err
		}
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

// insert is this command's one write, behind a savepoint, because the answer to
// a lost race with another registration of the same token is a sentence this
// transaction has to go on reading after it. A duplicate of the live-token key is
// a PostgreSQL error, and a PostgreSQL error leaves the transaction answering
// nothing at all until it ends — so the read-back that owes the caller the
// committed device would come back "current transaction is aborted", and a
// client retrying a registration it had in fact already made would be told an
// internal error and get no device. The savepoint is the half-rollback that keeps
// the transaction usable: on the way out the row is either written or unwound,
// and the command still has a transaction to read the winner's row in.
//
// The name is a literal and the savepoint is never released: Postgres discards
// what is still open at commit, and a second registration in one request
// transaction nests a savepoint of the same name, which ROLLBACK TO resolves to
// the inner one.
func (DeviceStore) insert(ctx context.Context, tx db.Tx[db.Tenant], row *contracts.Device) error {
	if err := tx.DB().SavePoint("notification_device_register").Error; err != nil {
		return err
	}
	created := crud.Create(ctx, tx, row)
	if created == nil {
		return nil
	}
	if back := tx.DB().RollbackTo("notification_device_register").Error; back != nil {
		// The insert failed and its unwind failed with it, which leaves the
		// transaction aborted and nothing above it able to commit: the unwind's
		// error is the one worth naming, and the insert's is the reason it happened.
		return fmt.Errorf("%w (the failed registration: %v)", back, created)
	}
	return created
}

// carries is the deployment's answer about one device, asked of the adapters this
// installation was built with, and it is asked before a row exists. An unknown
// platform spelling and a platform it carries no adapter for are both refused
// because a device nobody can send to is a row that fails every notice to that
// person; and so is a token that adapter refuses to carry, which is the question
// only the adapter can answer — the platform's token grammar, a subscription that
// is not a subscription, a key nothing can be encrypted for. A stored address an
// adapter will not carry fails every later notice to that person with an error no
// retry changes, and the person's client would have no way to learn that the
// token it holds is the problem.
func (s DeviceStore) carries(in contracts.DeviceInput) error {
	if err := in.Platform.Validate(); err != nil {
		return fmt.Errorf("%w: %w", crud.ErrInvalid, err)
	}
	adapter := s.Adapters.Get(in.Platform)
	if adapter == nil {
		return fmt.Errorf("%w: notification: this installation carries no %s push, so nothing could ever be sent to that device",
			crud.ErrInvalid, in.Platform)
	}
	if err := adapter.Validate(in.Token); err != nil {
		return fmt.Errorf("%w: notification: that token is not one this installation can carry: %w", crud.ErrInvalid, err)
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
//
// The row is read under its write lock, and the ownership question is asked of
// what that locked read answered. "Is this device yours" is a question about the
// row as it is when the deletion is written, not as it was when the read began:
// a device that moves to another person while this command waits behind their
// write would otherwise be deleted by the person who no longer holds the token,
// which is both a refusal that destroys and a device_removed event for a thing
// the caller has no claim on. The lock is held to the end of the transaction, so
// the answer cannot move between the read and the delete either.
func (s DeviceStore) RemoveDevice(ctx context.Context, tx db.Tx[db.Tenant], id, recipient uuid.UUID) error {
	if err := owning(ctx, recipient); err != nil {
		return err
	}
	row, err := crud.GetForUpdate[*contracts.Device](tx, id)
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
