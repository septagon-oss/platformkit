package contracts

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	user "github.com/septagon-oss/platformkit/modules/user/contracts"
)

const (
	InvitationRecoveryRequest = "recovery"
	InvitationLegacyRequest   = "legacy_initial"
)

// InvitationSource is the original owner-stamped intent consumed by a worker.
// It is trusted event data, never a public request body.
type InvitationSource struct {
	ID   uuid.UUID
	At   time.Time
	Kind string
}

// InvitationUsers is auth's companion to Users for managed invitation transitions.
// The person row is locked before any password token row; no method commits tx.
type InvitationUsers interface {
	LockInvitation(context.Context, db.Tx[db.Tenant], uuid.UUID) (*user.User, error)
	AcceptInvitationPassword(context.Context, db.Tx[db.Tenant], uuid.UUID, int64, string, time.Time) error
	RequestInvitationLink(context.Context, db.Tx[db.Tenant], uuid.UUID, InvitationSource) error
}

// InvitationLinks sends only the explicitly named, still-current generation.
type InvitationLinks interface {
	OfferInvitation(context.Context, db.Tx[db.Tenant], uuid.UUID, int64, uuid.UUID) error
}

// InvitationReading separates the mail this invitation committed to from what a
// transport reported about it. The observation answers only what this build can
// prove — no durable mail-attempt record exists here, so a queued handoff stays
// unobserved and a legacy one unknown — and it never claims a message arrived.
// Provider error text, credentials and internal delivery identities are absent.
type InvitationReading struct {
	*user.InvitationSnapshot
	Request     string `json:"request"`
	Observation string `json:"observation"`
}

// InvitationReadings authorizes through the user owner and correlates only the
// delivery in that returned snapshot. A ledger error returns no partial answer.
type InvitationReadings interface {
	GetInvitationReading(context.Context, db.Tx[db.Tenant], uuid.UUID) (*InvitationReading, error)
	ListInvitationReadings(context.Context, db.Tx[db.Tenant], user.InvitationQuery) ([]*InvitationReading, error)
}

// InvitationOfferEligible is the shared fence before creating a token or handoff.
func InvitationOfferEligible(u *user.User, generation int64, delivery uuid.UUID, at time.Time) bool {
	return u != nil && u.DeletedAt == nil && u.Status == user.StatusInvited && generation > 0 &&
		u.Invitation.Generation == generation && u.Invitation.DeliveryID != nil && *u.Invitation.DeliveryID == delivery &&
		u.Invitation.Recipient == EmailKey(u.Email) && u.Invitation.RequestedAt != nil && !at.Before(*u.Invitation.RequestedAt) &&
		user.InvitationState(u.Invitation, at) == user.InvitationPending
}
