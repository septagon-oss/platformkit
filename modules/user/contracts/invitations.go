package contracts

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

const (
	InvitationPending          = "pending"
	InvitationExpired          = "expired"
	InvitationAccepted         = "accepted"
	InvitationRevoked          = "revoked"
	InvitationPasswordLink     = "password_link"
	InvitationDirectPassword   = "direct_password"
	InvitationIdentityProvider = "identity_provider"
)

var (
	ErrInvitationUnavailable     = errors.New("user: invitation capability is unavailable")
	ErrInvitationForbidden       = errors.New("user: current authority does not permit this invitation operation")
	ErrInvitationUnauthenticated = errors.New("user: invitation operation requires an authenticated actor")
	ErrInvitationInitializing    = errors.New("user: invitation_history_initializing")
)

// Clock supplies decision time. Owners normalize it to UTC microseconds after locks.
type Clock interface{ Now() time.Time }

// InvitationTiming comes from the composition's existing token policy.
// Nonpositive values make managed invitation operations unavailable.
type InvitationTiming struct{ Lifetime, ResendInterval time.Duration }

// InvitationHistory is command-owned metadata on the existing tenant person.
// A zero value records no invitation; expiry is derived without a write.
type InvitationHistory struct {
	Phase       string `gorm:"default:null"`
	Revision    int64
	Generation  int64
	Recipient   string `gorm:"default:null"`
	AdoptedAt   *time.Time
	RequestedAt *time.Time
	ExpiresAt   *time.Time
	DeliveryID  *uuid.UUID
	AcceptedAt  *time.Time
	AcceptedVia *string
	RevokedAt   *time.Time
}

// InvitationSnapshot is an authorized, credential-free reading at AsOf. DeliveryID and
// Generation are trusted integration data and never part of its public JSON.
type InvitationSnapshot struct {
	UserID          uuid.UUID  `json:"userId"`
	Recipient       string     `json:"recipient"`
	DisplayName     string     `json:"displayName"`
	AssignedRoles   Roles      `json:"assignedRoles"`
	MemberStatus    string     `json:"memberStatus"`
	State           string     `json:"state"`
	Revision        int64      `json:"revision"`
	AsOf            time.Time  `json:"asOf"`
	RequestedAt     *time.Time `json:"requestedAt"`
	ExpiresAt       *time.Time `json:"expiresAt"`
	AcceptedAt      *time.Time `json:"acceptedAt"`
	AcceptedVia     string     `json:"acceptedVia,omitempty"`
	RevokedAt       *time.Time `json:"revokedAt"`
	LegacyAdoptedAt *time.Time `json:"legacyAdoptedAt"`
	Legacy          bool       `json:"legacy"`
	PermittedRoles  []string   `json:"permittedRoles"`
	CanResend       bool       `json:"canResend"`
	CanRevoke       bool       `json:"canRevoke"`
	CanEditRoles    bool       `json:"canEditRoles"`
	NextResendAt    *time.Time `json:"nextResendAt"`
	Generation      int64      `json:"-"`
	DeliveryID      *uuid.UUID `json:"-"`
}

// InvitationIntent creates one invited person with the roles assigned atomically.
type InvitationIntent struct {
	Email, DisplayName string
	Roles              []string
}

// InvitationCommands checks current actor authority and expected revision inside
// tx. Refusals return no subject and write no domain state or events.
type InvitationCommands interface {
	CreateInvitation(context.Context, db.Tx[db.Tenant], InvitationIntent) (*User, error)
	ResendInvitation(context.Context, db.Tx[db.Tenant], uuid.UUID, int64) (*InvitationSnapshot, error)
	RevokeInvitation(context.Context, db.Tx[db.Tenant], uuid.UUID, int64) (*InvitationSnapshot, error)
	SetInvitationRoles(context.Context, db.Tx[db.Tenant], uuid.UUID, int64, []string) (*User, error)
	SetInvitationPassword(context.Context, db.Tx[db.Tenant], uuid.UUID, int64, string) error
}

// InvitationQuery bounds an authorized list: zero limit defaults to 25, maximum
// 100, nonnegative offset, and an empty state or one InvitationSnapshot state.
type InvitationQuery struct {
	Limit, Offset int
	State         string
}

// InvitationReader omits removed people and returns no partial answer on failure.
type InvitationReader interface {
	GetInvitation(context.Context, db.Tx[db.Tenant], uuid.UUID) (*InvitationSnapshot, error)
	ListInvitations(context.Context, db.Tx[db.Tenant], InvitationQuery) ([]*InvitationSnapshot, error)
}

// InvitationRetention locks the retained person before erasure contributors run.
// Waiting invitations must be revoked first; floor refusals write nothing.
type InvitationRetention interface {
	LockForErasure(context.Context, db.Tx[db.Tenant], uuid.UUID) (*User, error)
}

// InvitationAccess is current authority, resolved from fresh actor and role rows.
// KnownRoles validates names; GrantableRoles limits additions, never removals.
type InvitationAccess struct {
	Read, Manage, GrantAdmin                  bool
	KnownRoles, GrantableRoles, Administering []string
}

// InvitationAuthority resolves the actor's live role grants and credential scope.
// The caller holds the tenant administration lock and supplies a fresh actor.
type InvitationAuthority interface {
	InspectInvitationAuthority(context.Context, db.Tx[db.Tenant], tenancy.Principal, *User) (InvitationAccess, error)
}

// InvitationAuthorityFunc is a comparable holder filled by the grant owner during
// declared composition. An unfilled holder fails closed on every operation.
type InvitationAuthorityFunc struct {
	Ask func(context.Context, db.Tx[db.Tenant], tenancy.Principal, *User) (InvitationAccess, error)
}

func (a *InvitationAuthorityFunc) InspectInvitationAuthority(ctx context.Context, tx db.Tx[db.Tenant], p tenancy.Principal, u *User) (InvitationAccess, error) {
	if a == nil || a.Ask == nil {
		return InvitationAccess{}, ErrInvitationUnavailable
	}
	return a.Ask(ctx, tx, p, u)
}

// InvitationCooldown preserves the previous request time and names the first
// instant a replacement may be requested. It carries no subject or revision.
type InvitationCooldown struct{ RetryAt time.Time }

func (e *InvitationCooldown) Error() string { return "user: invitation resend is cooling down" }

// DecideInvitationRequest fences a trusted recovery or legacy event by its
// original timestamp. Duplicate, cooling and obsolete requests change nothing.
func DecideInvitationRequest(u *User, kind string, requested time.Time, delivery uuid.UUID, timing InvitationTiming, at time.Time) (*User, bool, error) {
	if requested.IsZero() || requested.After(at) {
		return nil, false, ErrInvitationUnavailable
	}
	if kind != "recovery" && kind != "legacy_initial" {
		return nil, false, crud.ErrInvalid
	}
	if err := CheckInvitationTime(u.Invitation, at); err != nil {
		return nil, false, err
	}
	if u.Status != StatusInvited || u.Invitation.Phase != InvitationPending {
		return u, false, nil
	}
	if u.Invitation.RequestedAt != nil && !requested.After(*u.Invitation.RequestedAt) {
		return u, false, nil
	}
	if kind == "legacy_initial" {
		if u.Invitation.Generation != 0 || InvitationState(u.Invitation, at) != InvitationPending {
			return u, false, nil
		}
		next := *u
		next.Invitation = InvitationHistory{}
		history, err := BeginInvitation(&next, timing, at, delivery, false)
		if err != nil {
			return nil, false, err
		}
		history.AdoptedAt, history.Revision = u.Invitation.AdoptedAt, u.Invitation.Revision+1
		next.Invitation = history
		return &next, true, nil
	}
	next, changed, err := DecideInvitation(u, InvitationMutation{Action: InvitationResend, ExpectedRevision: u.Invitation.Revision, DeliveryID: delivery}, timing, at)
	if _, cooling := errors.AsType[*InvitationCooldown](err); cooling {
		return u, false, nil
	}
	return next, changed, err
}

const (
	EventInvitationResent   = "user.invitation_resent"
	EventInvitationRevoked  = "user.invitation_revoked"
	EventInvitationAccepted = "user.invitation_accepted"
)

// InvitationTransition records a committed generation or terminal transition.
// It contains neither a password nor the token or digest used to accept it.
type InvitationTransition struct {
	UserID      uuid.UUID  `json:"userId"`
	Generation  int64      `json:"generation"`
	Revision    int64      `json:"revision"`
	DeliveryID  *uuid.UUID `json:"deliveryId,omitempty"`
	At          time.Time  `json:"at"`
	AcceptedVia *string    `json:"acceptedVia,omitempty"`
	Served      string     `json:"served,omitempty"`
}

// ReadInvitation projects a known recorded history and advisory authority at now.
// Callers check authorization and history presence before returning the projection.
func ReadInvitation(u *User, access InvitationAccess, timing InvitationTiming, now time.Time) *InvitationSnapshot {
	h := u.Invitation.Clone()
	state := InvitationState(h, now)
	waiting := state == InvitationPending || state == InvitationExpired
	r := &InvitationSnapshot{UserID: u.ID, Recipient: h.Recipient, DisplayName: u.DisplayName, AssignedRoles: slices.Clone(u.Roles),
		MemberStatus: u.Status, State: state, Revision: h.Revision, AsOf: now, RequestedAt: h.RequestedAt, ExpiresAt: h.ExpiresAt,
		AcceptedAt: h.AcceptedAt, AcceptedVia: acceptedVia(h), RevokedAt: h.RevokedAt, LegacyAdoptedAt: h.AdoptedAt,
		Legacy: h.AdoptedAt != nil, Generation: h.Generation, DeliveryID: h.DeliveryID, PermittedRoles: slices.Clone(access.GrantableRoles),
		CanRevoke: access.Manage && waiting, CanEditRoles: access.Manage && waiting}
	requested := h.RequestedAt
	if requested == nil {
		requested = h.AdoptedAt
	}
	if requested != nil && waiting {
		r.NextResendAt = new(requested.Add(timing.ResendInterval))
		r.CanResend = access.Manage && !now.Before(*r.NextResendAt)
	}
	return r
}

// CheckInvitationQuery applies the public bounds and returns the defaulted query.
func CheckInvitationQuery(q InvitationQuery) (InvitationQuery, error) {
	if q.Limit < 0 || q.Limit > 100 || q.Offset < 0 || !slices.Contains([]string{"", InvitationPending, InvitationExpired, InvitationAccepted, InvitationRevoked}, q.State) {
		return InvitationQuery{}, fmt.Errorf("%w: invalid invitation page", crud.ErrInvalid)
	}
	if q.Limit == 0 {
		q.Limit = 25
	}
	return q, nil
}

func acceptedVia(h InvitationHistory) string {
	if h.AcceptedVia == nil {
		return ""
	}
	return *h.AcceptedVia
}

// Clone detaches optional timestamps and identities from a stored history snapshot.
func (h InvitationHistory) Clone() InvitationHistory {
	clone := func(at *time.Time) *time.Time {
		if at == nil {
			return nil
		}
		return new(*at)
	}
	h.AdoptedAt, h.RequestedAt, h.ExpiresAt = clone(h.AdoptedAt), clone(h.RequestedAt), clone(h.ExpiresAt)
	h.AcceptedAt, h.RevokedAt = clone(h.AcceptedAt), clone(h.RevokedAt)
	if h.DeliveryID != nil {
		h.DeliveryID = new(*h.DeliveryID)
	}
	if h.AcceptedVia != nil {
		h.AcceptedVia = new(*h.AcceptedVia)
	}
	return h
}
