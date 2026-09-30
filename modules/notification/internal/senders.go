package internal

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
)

// Senders is contracts.Senders and contracts.SenderAdmin over this module's
// notification_senders row: the tenant's own, decided by the transaction, never
// by a name in a config file, because one binary serves many tenants and the
// address in a mail header belongs to one of them.
//
// It holds the two collaborators this module cannot be the authority about:
// Verifier is what decides that a domain really says so (contracts.SenderVerifier
// — the names to query and the timeout are the product's, not this module's),
// and Keys is where the signing key for a domain lives. Both may be nil, and
// both refusals are a suppression with a reason rather than a panic: a
// deployment with no resolver can still be told it cannot verify anything, and
// a deployment with no key can still write the row and say why nothing is sent.
type Senders struct {
	Verifier contracts.SenderVerifier
	Keys     contracts.DKIMKeys
}

var (
	_ contracts.Senders     = (*Senders)(nil)
	_ contracts.SenderAdmin = (*Senders)(nil)
)

// For is the tenant's sender, or nil when it never set one — an answer, not an
// error, and the answer Decide reads as "this tenant sends no mail". The
// deployment's key is attached to the row here and nowhere else, so a caller
// that read a sender cannot accidentally hold one it cannot sign with, and
// cannot accidentally be handed one by a table.
func (s *Senders) For(ctx context.Context, tx db.Tx[db.Tenant]) (*contracts.Sender, error) {
	var row contracts.Sender
	err := tx.DB().Where("deleted_at IS NULL").Take(&row).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return nil, nil
	case err != nil:
		return nil, crud.Classify(err)
	}
	if s.Keys != nil {
		row.Key = s.Keys.KeyFor(ctx, tx, row)
	}
	return &row, nil
}

// Put saves the tenant's sender, creating it or replacing what is there, and it
// is the only way a row comes to be pending.
//
// Status is never taken from the caller: an address somebody typed is not a
// domain that consents to being signed for, and a tenant that could post
// "verified" needs no DNS. What the caller may change freely is the display
// name, the address and the reply-to of a sender that is already believed — the
// proof is about the (domain, selector) pair it was made for, so the pair
// changing takes the row back to pending with a fresh token, and the pair
// staying the same keeps the verification it already earned.
func (s *Senders) Put(ctx context.Context, tx db.Tx[db.Tenant], in contracts.Sender) (*contracts.Sender, error) {
	in.Status = contracts.SenderPending // whatever the caller asked for is refused here
	in.Key = nil
	existing, err := s.live(tx)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		in.ID, in.TenantID = existing.ID, existing.TenantID
		if existing.Status == contracts.SenderVerified && existing.Domain == in.Domain && existing.Selector == in.Selector {
			in.Status, in.Token, in.Proof, in.VerifiedAt = existing.Status, existing.Token, existing.Proof, existing.VerifiedAt
		}
	}
	if in.Token == "" {
		if in.Token, err = mintToken(); err != nil {
			return nil, err
		}
	}
	if err := in.Validate(ctx); err != nil {
		return nil, fmt.Errorf("%w: %w", crud.ErrInvalid, err)
	}
	if existing == nil {
		if err := crud.Create(ctx, tx, &in); err != nil {
			return nil, err
		}
	} else if err := crud.Update(ctx, tx, &in,
		"domain", "selector", "from_name", "from_address", "reply_to", "status", "token", "proof", "verified_at", "updated_at"); err != nil {
		return nil, err
	}
	actor, _ := tenancy.ActorFrom(ctx)
	return &in, events.Publish(ctx, tx, contracts.EventSenderSet, contracts.SenderSet{
		SenderID: in.ID, Domain: in.Domain, Selector: in.Selector, Status: in.Status, Actor: actor, At: db.Now(),
	})
}

// Verify asks the composition whether the domain says so and, when it does,
// records what the check saw. A refusal leaves the row pending, writes nothing
// and publishes nothing, and the error is the sentence an operator acts on.
//
// The row is read under its own lock first: two people clicking "verify" at the
// same moment must not both see pending, and the second must find the row
// already believed rather than run a second check into a second proof.
func (s *Senders) Verify(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*contracts.Sender, error) {
	if s.Verifier == nil {
		return nil, fmt.Errorf("notification: this deployment has no way to check a sender's domain")
	}
	row, err := crud.GetForUpdate[*contracts.Sender](tx, id)
	if err != nil {
		return nil, err
	}
	if row.Status == contracts.SenderVerified {
		return row, nil
	}
	proof, err := s.Verifier.Verify(ctx, *row)
	if err != nil {
		return nil, fmt.Errorf("notification: %s says it is not verified: %w", row.VerificationName(), err)
	}
	if proof == "" {
		return nil, fmt.Errorf("notification: the sender check returned no proof, and a verification nobody can explain is not one")
	}
	at := db.Now()
	row.Status, row.Proof, row.VerifiedAt = contracts.SenderVerified, proof, &at
	if err := crud.Update(ctx, tx, row, "status", "proof", "verified_at", "updated_at"); err != nil {
		return nil, err
	}
	actor, _ := tenancy.ActorFrom(ctx)
	return row, events.Publish(ctx, tx, contracts.EventSenderVerified, contracts.Verified{
		SenderID: row.ID, Domain: row.Domain, Selector: row.Selector, Actor: actor, At: at,
	})
}

// Delete removes a sender that is not believed.
//
// A verified one is refused: this is the write that would take the last one
// away, and a tenant whose only verified sender is gone cannot be told anything
// by mail until a fresh domain is checked, which is hours of somebody else's
// DNS. The correction is to Put the replacement first, which the message says.
// Deleting nothing deletes nothing: an unknown id is ErrNotFound, the same
// answer another tenant's id gives.
func (s *Senders) Delete(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) error {
	row, err := crud.GetForUpdate[*contracts.Sender](tx, id)
	if err != nil {
		return err
	}
	if row.Status == contracts.SenderVerified {
		return fmt.Errorf("%w: %s is this tenant's verified sender; save its replacement before removing it",
			crud.ErrConflict, row.Header())
	}
	return crud.Delete[*contracts.Sender](tx, id, true)
}

// live is the tenant's one live sender row, or nil.
func (s *Senders) live(tx db.Tx[db.Tenant]) (*contracts.Sender, error) {
	var row contracts.Sender
	err := tx.DB().Where("deleted_at IS NULL").Take(&row).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return nil, nil
	case err != nil:
		return nil, crud.Classify(err)
	}
	return &row, nil
}

// mintToken is the 32 random bytes the domain has to publish. A value an
// attacker could guess is not a proof that the domain asked for this, so it
// comes from crypto/rand; a failure there is the platform's, not the caller's.
func mintToken() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("notification: mint the sender verification token: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}
