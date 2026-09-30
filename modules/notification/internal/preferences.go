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
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
)

// Prefs is contracts.Preferences and contracts.PreferenceService over this
// module's own two tables, read and written in the caller's transaction.
//
// It holds nothing, because there is nothing to hold: every answer is a query
// under the tenant of the transaction it is handed, so a composition needs no
// cache invalidation and a test needs no fixture beyond the schema.
//
// The two interfaces are one type because they read and write the same rows:
// splitting them would be two structs with the same methods on the same table,
// and the split the contracts make is between the notice path (Preferences) and
// the person (PreferenceService), which is a difference of who holds the port,
// not of where the rows are.
type Prefs struct{}

var (
	_ contracts.Preferences       = Prefs{}
	_ contracts.PreferenceService = Prefs{}
)

// Settings is this person's rows for one intent plus their blanket ones, the
// intent's own first so contracts.Enabled can take the first answer it finds.
// No rows is the normal answer, and it means every channel is on.
func (Prefs) Settings(_ context.Context, tx db.Tx[db.Tenant], recipient uuid.UUID, intent string) ([]contracts.Preference, error) {
	var out []contracts.Preference
	err := tx.DB().
		Where("recipient_id = ? AND intent IN (?, '') AND deleted_at IS NULL", recipient, intent).
		Order("intent DESC, channel").Find(&out).Error
	return out, err
}

// Quiet is the person's window, or nil when they never set one.
func (Prefs) Quiet(_ context.Context, tx db.Tx[db.Tenant], recipient uuid.UUID) (*contracts.QuietHours, error) {
	var row contracts.QuietHours
	err := tx.DB().Where("recipient_id = ? AND deleted_at IS NULL", recipient).Take(&row).Error
	switch {
	case errors.Is(err, nil):
		return &row, nil
	case errors.Is(err, gorm.ErrRecordNotFound):
		return nil, nil
	default:
		return nil, crud.Classify(err)
	}
}

// SetChannel writes one answer. It is idempotent in the way a command has to
// be: the row already says this, so nothing is written and nothing is
// published — a second click is not two rows in the audit trail and one person
// turning mail off twice is not two facts.
//
// The row is read under the transaction's own lock before it is written, so two
// requests that both ask "is there a row yet?" cannot both answer no and both
// insert; the unique index is the backstop that makes the loser's transaction
// fail rather than the table hold two answers.
//
// The recipient comes from the caller's principal, never from the body: there is
// no shape of this call that writes another person's settings, which is the same
// fact as Service.ListFor scoping by the caller.
func (p Prefs) SetChannel(ctx context.Context, tx db.Tx[db.Tenant], recipient uuid.UUID, intent string, channel contracts.Channel, enabled bool) (*contracts.Preference, error) {
	if err := channel.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %w", crud.ErrInvalid, err)
	}
	if channel == contracts.ChannelInApp {
		return nil, fmt.Errorf("%w: in-app is not a preference: the row is the notice", crud.ErrInvalid)
	}
	row, err := p.locked(tx, recipient, intent, channel)
	if err != nil {
		return nil, err
	}
	if row != nil && row.Enabled == enabled {
		return row, nil
	}
	at := db.Now()
	if row == nil {
		row = &contracts.Preference{RecipientID: recipient, Intent: intent, Channel: channel, Enabled: enabled}
		if err := crud.Create(ctx, tx, row); err != nil {
			return nil, err
		}
	} else {
		row.Enabled = enabled
		if err := crud.Update(ctx, tx, row, "enabled", "updated_at"); err != nil {
			return nil, err
		}
	}
	return row, events.Publish(ctx, tx, contracts.EventPreferenceSet, contracts.PreferenceSet{
		Recipient: recipient, Intent: intent, Channel: channel, Enabled: enabled, At: at,
	})
}

// locked is the person's one answer for (intent, channel), or nil when there is
// none, with the row's write lock held if there is one.
func (Prefs) locked(tx db.Tx[db.Tenant], recipient uuid.UUID, intent string, channel contracts.Channel) (*contracts.Preference, error) {
	var row contracts.Preference
	err := tx.DB().Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("recipient_id = ? AND intent = ? AND channel = ? AND deleted_at IS NULL", recipient, intent, string(channel)).
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

// SetQuietHours writes the person's one window and refuses a zone or a minute
// it could not read back (QuietHours.Validate, which crud runs on the write).
// Setting the window that is already set changes nothing and publishes nothing,
// for the reason SetChannel gives.
func (p Prefs) SetQuietHours(ctx context.Context, tx db.Tx[db.Tenant], q contracts.QuietHours) (*contracts.QuietHours, error) {
	row, err := p.lockedQuiet(tx, q.RecipientID)
	if err != nil {
		return nil, err
	}
	at := db.Now()
	if row == nil {
		row = &q
		if err := crud.Create(ctx, tx, row); err != nil {
			return nil, err
		}
		return row, events.Publish(ctx, tx, contracts.EventPreferenceSet, contracts.PreferenceSet{
			Recipient: q.RecipientID, At: at,
		})
	}
	if row.StartMinute == q.StartMinute && row.EndMinute == q.EndMinute && row.TimeZone == q.TimeZone {
		return row, nil
	}
	row.StartMinute, row.EndMinute, row.TimeZone = q.StartMinute, q.EndMinute, q.TimeZone
	if err := crud.Update(ctx, tx, row, "start_minute", "end_minute", "time_zone", "updated_at"); err != nil {
		return nil, err
	}
	return row, events.Publish(ctx, tx, contracts.EventPreferenceSet, contracts.PreferenceSet{
		Recipient: q.RecipientID, At: at,
	})
}

// ClearQuietHours removes the window. Having none is the state a person who
// never set one is in, so there is nothing to refuse when the row is not there,
// and nothing to publish when nothing changed.
func (p Prefs) ClearQuietHours(ctx context.Context, tx db.Tx[db.Tenant], recipient uuid.UUID) error {
	row, err := p.lockedQuiet(tx, recipient)
	if err != nil || row == nil {
		return err
	}
	if err := crud.Delete[*contracts.QuietHours](tx, row.ID, true); err != nil {
		return err
	}
	return events.Publish(ctx, tx, contracts.EventPreferenceSet, contracts.PreferenceSet{
		Recipient: recipient, At: db.Now(),
	})
}

func (Prefs) lockedQuiet(tx db.Tx[db.Tenant], recipient uuid.UUID) (*contracts.QuietHours, error) {
	var row contracts.QuietHours
	err := tx.DB().Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("recipient_id = ? AND deleted_at IS NULL", recipient).Take(&row).Error
	switch {
	case errors.Is(err, nil):
		return &row, nil
	case errors.Is(err, gorm.ErrRecordNotFound):
		return nil, nil
	default:
		return nil, crud.Classify(err)
	}
}

// Mine is the person's own rows: the blanket answers first, then intent and
// channel, so a settings page renders the general switch above the exceptions
// to it. The recipient is set here and not taken from the query, which is what
// makes there be no query that lists another person's choices.
func (Prefs) Mine(_ context.Context, tx db.Tx[db.Tenant], recipient uuid.UUID, q crud.Query) ([]*contracts.Preference, int64, error) {
	q.Filter = map[string]any{"recipientId": recipient}
	if q.Sort == "" {
		q.Sort = "intent"
	}
	return crud.List[*contracts.Preference](tx, q)
}
