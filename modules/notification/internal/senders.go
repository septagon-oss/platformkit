package internal

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

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
	// Grants answers whether the caller of a sender command holds
	// contracts.PermissionSenderManage. Nil means the deployment has no answer, and
	// a signed-in caller is refused on it — see contracts.GrantChecker.
	Grants contracts.GrantChecker
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
	actor, err := s.administered(ctx, tx)
	if err != nil {
		return nil, err
	}
	in.Status = contracts.SenderPending // whatever the caller asked for is refused here
	in.Key = nil
	existing, err := s.locked(tx)
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
	return &in, events.Publish(ctx, tx, contracts.EventSenderSet, contracts.SenderSet{
		SenderID: in.ID, Domain: in.Domain, Selector: in.Selector, Status: in.Status, Actor: actor, At: db.Now(),
	})
}

// administered is the caller one of these three commands has to be able to name,
// and to hold the key that says they may.
//
// tenancy.ActorFrom is unset for work with no person behind it — a job, the
// relay, a retried event — and for a read that is the honest answer rather than a
// sentinel, which is why For and the notice's own decision ask nothing about it.
// For these three it is not enough: what they change is the address this tenant
// may put in a From header and the selector its mail signs under, and the answer
// to "who set this" lives in the event these commands publish. A write whose own
// audit row would name nobody has not rechecked its caller, so the command is
// refused before the row is read: it writes nothing, publishes nothing and
// returns no row, which is what contracts.SenderAdmin promises.
//
// A caller who arrives as a signed-in principal is asked the second question the
// grant is for: their roles have to grant contracts.PermissionSenderManage, answered
// by the composition (contracts.GrantChecker) because what a role name grants is the
// auth module's fact and not this module's. A deployment that wires no checker has
// no answer to give, so a person is refused rather than assumed in — house rule 9's
// recheck, inside the authoritative transaction, which is the half a route's guard
// cannot do. Work with no principal at all is platform work, authorized by whoever
// composed the job that runs it, and is not asked this question.
func (s *Senders) administered(ctx context.Context, tx db.Tx[db.Tenant]) (uuid.UUID, error) {
	actor, ok := tenancy.ActorFrom(ctx)
	if !ok {
		return uuid.Nil, fmt.Errorf("%w: notification: this tenant's sender is changed by a caller its own transaction names, and this one names nobody", tenancy.ErrPolicyDenied)
	}
	principal, signedIn := tenancy.PrincipalFrom(ctx)
	if !signedIn {
		return actor, nil
	}
	if principal.UserID != actor {
		return uuid.Nil, fmt.Errorf("%w: notification: this caller's session is one person and its actor is another", tenancy.ErrPolicyDenied)
	}
	if s.Grants == nil {
		return uuid.Nil, fmt.Errorf("%w: notification: this deployment wires no way to check %s, so a signed-in caller cannot be granted it",
			tenancy.ErrPolicyDenied, contracts.PermissionSenderManage)
	}
	held, err := s.Grants.Holds(ctx, tx, contracts.PermissionSenderManage)
	if err != nil {
		return uuid.Nil, fmt.Errorf("notification: check %s: %w", contracts.PermissionSenderManage, err)
	}
	if !held {
		return uuid.Nil, fmt.Errorf("%w: notification: this caller's roles do not grant %s", tenancy.ErrPolicyDenied, contracts.PermissionSenderManage)
	}
	return actor, nil
}

// Verify asks the composition whether the domain says so and, when it does,
// records what the check saw. A refusal leaves the row pending, writes nothing
// and publishes nothing, and the error is the sentence an operator acts on.
//
// The row is read under its own lock first: two people clicking "verify" at the
// same moment must not both see pending, and the second must find the row
// already believed rather than run a second check into a second proof.
func (s *Senders) Verify(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*contracts.Sender, error) {
	actor, err := s.administered(ctx, tx)
	if err != nil {
		return nil, err
	}
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
	actor, err := s.administered(ctx, tx)
	if err != nil {
		return err
	}
	row, err := crud.GetForUpdate[*contracts.Sender](tx, id)
	if err != nil {
		return err
	}
	if row.Status == contracts.SenderVerified {
		return fmt.Errorf("%w: %s is this tenant's verified sender; save its replacement before removing it",
			crud.ErrConflict, row.Header())
	}
	if err := crud.Delete[*contracts.Sender](tx, id, true); err != nil {
		return err
	}
	// The removal is as much a fact about this tenant's mail identity as the write
	// that made it, and modules/audit records published events and not rows: a
	// deletion that publishes nothing is one nobody can attribute afterwards.
	return events.Publish(ctx, tx, contracts.EventSenderRemoved, contracts.SenderRemoved{
		SenderID: row.ID, Domain: row.Domain, Selector: row.Selector, Actor: actor, At: db.Now(),
	})
}

// locked is the tenant's one live sender row, or nil, with the row's write lock
// held if there is one.
//
// Put reads the row it replaces under its own lock because what it writes is
// decided by what it read: the status, token and proof of a sender on the same
// (domain, selector) pair survive the write, and a read with no lock can be
// answered from a moment that has already gone. A Verify that commits between
// that read and this write is then undone — status back to pending, proof and
// verified_at cleared — with no error to anybody and a sender_set row that says
// "pending", which is the trail of a write nobody asked for. Verify and Delete
// take the same lock on the same row, so the three commands cannot interleave.
func (s *Senders) locked(tx db.Tx[db.Tenant]) (*contracts.Sender, error) {
	var row contracts.Sender
	err := tx.DB().Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("deleted_at IS NULL").Take(&row).Error
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
