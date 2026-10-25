package contracts

import (
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
)

// InvitationState classifies recorded history at now without changing its revision.
func InvitationState(h InvitationHistory, now time.Time) string {
	if h.Phase == InvitationPending && h.ExpiresAt != nil && !now.Before(*h.ExpiresAt) {
		return InvitationExpired
	}
	return h.Phase
}

// BeginInvitation records a request, or adopts legacy history without inventing
// a request. The caller validates the user and supplies a fresh delivery identity.
func BeginInvitation(u *User, timing InvitationTiming, now time.Time, delivery uuid.UUID, legacy bool) (InvitationHistory, error) {
	if timing.Lifetime <= 0 || timing.ResendInterval <= 0 || now.IsZero() {
		return InvitationHistory{}, ErrInvitationUnavailable
	}
	if u == nil || u.Status != StatusInvited || u.Invitation.Phase != "" {
		return InvitationHistory{}, crud.ErrConflict
	}
	now = now.UTC().Truncate(time.Microsecond)
	h := InvitationHistory{Phase: InvitationPending, Revision: 1, Recipient: CanonicalEmail(u.Email), ExpiresAt: new(now.Add(timing.Lifetime))}
	if legacy {
		h.AdoptedAt = new(now)
		return h, nil
	}
	if delivery == uuid.Nil {
		return InvitationHistory{}, ErrInvitationUnavailable
	}
	h.Generation, h.DeliveryID, h.RequestedAt = 1, new(delivery), new(now)
	return h, nil
}

// InvitationMutation is one owner intention. Password hashing and persistence
// remain outside this pure rule. Acceptance uses Generation instead of a revision.
type InvitationMutation struct {
	Action           string
	ExpectedRevision int64
	Generation       int64
	Roles            []string
	DeliveryID       uuid.UUID
}

const (
	InvitationResend = "resend"
	InvitationRevoke = "revoke"
	InvitationRoles  = "roles"
)

// DecideInvitation returns a detached next user and whether it changes. It never
// modifies its input; a refusal returns no user, including stale would-be no-ops.
func DecideInvitation(u *User, intent InvitationMutation, timing InvitationTiming, now time.Time) (*User, bool, error) {
	if u == nil || u.DeletedAt != nil {
		return nil, false, crud.ErrNotFound
	}
	if timing.Lifetime <= 0 || timing.ResendInterval <= 0 {
		return nil, false, ErrInvitationUnavailable
	}
	if err := CheckInvitationTime(u.Invitation, now); err != nil {
		return nil, false, err
	}
	if u.Invitation.Phase == "" {
		return nil, false, ErrInvitationInitializing
	}
	accept := slices.Contains([]string{InvitationPasswordLink, InvitationDirectPassword, InvitationIdentityProvider}, intent.Action)
	if !accept || intent.Action == InvitationDirectPassword {
		if intent.ExpectedRevision <= 0 {
			return nil, false, fmt.Errorf("%w: expectedRevision must be positive", crud.ErrInvalid)
		}
		if intent.ExpectedRevision != u.Invitation.Revision {
			return nil, false, crud.ErrConflict
		}
	}
	next := *u
	next.Roles = slices.Clone(u.Roles)
	now = now.UTC().Truncate(time.Microsecond)
	if u.Invitation.Phase == InvitationRevoked && intent.Action == InvitationRevoke {
		return &next, false, nil
	}
	if u.Invitation.Phase != InvitationPending || u.Status != StatusInvited {
		return nil, false, crud.ErrConflict
	}
	var err error
	switch intent.Action {
	case InvitationResend:
		err = replaceInvitation(&next, intent.DeliveryID, timing, now)
	case InvitationRevoke:
		next.Invitation.Phase, next.Invitation.RevokedAt, next.Status = InvitationRevoked, new(now), StatusInactive
	case InvitationRoles:
		next.Roles = CanonicalRoles(intent.Roles)
		if slices.Equal(next.Roles, u.Roles) {
			return &next, false, nil
		}
	case InvitationPasswordLink, InvitationDirectPassword, InvitationIdentityProvider:
		err = admitInvitation(&next, intent, now)
	default:
		err = fmt.Errorf("%w: unknown invitation operation", crud.ErrInvalid)
	}
	if err != nil {
		return nil, false, err
	}
	next.Invitation.Revision++
	return &next, true, nil
}

// CheckInvitationTime refuses an unavailable clock or an instant before recorded history.
func CheckInvitationTime(h InvitationHistory, now time.Time) error {
	if now.IsZero() {
		return ErrInvitationUnavailable
	}
	for _, at := range []*time.Time{h.RequestedAt, h.AdoptedAt, h.AcceptedAt, h.RevokedAt} {
		if at != nil && now.Before(*at) {
			return ErrInvitationUnavailable
		}
	}
	return nil
}

func replaceInvitation(u *User, delivery uuid.UUID, timing InvitationTiming, now time.Time) error {
	if timing.Lifetime <= 0 || timing.ResendInterval <= 0 || delivery == uuid.Nil {
		return ErrInvitationUnavailable
	}
	previous := u.Invitation.RequestedAt
	if previous == nil {
		previous = u.Invitation.AdoptedAt
	}
	if previous == nil {
		return ErrInvitationInitializing
	}
	if retry := previous.Add(timing.ResendInterval); now.Before(retry) {
		return &InvitationCooldown{RetryAt: retry}
	}
	u.Invitation.Generation++
	u.Invitation.RequestedAt, u.Invitation.ExpiresAt = new(now), new(now.Add(timing.Lifetime))
	u.Invitation.DeliveryID = new(delivery)
	return nil
}

func admitInvitation(u *User, intent InvitationMutation, now time.Time) error {
	if InvitationState(u.Invitation, now) != InvitationPending || u.Invitation.Recipient != CanonicalEmail(u.Email) {
		return crud.ErrConflict
	}
	if intent.Action == InvitationPasswordLink && intent.Generation != u.Invitation.Generation {
		return crud.ErrConflict
	}
	u.Invitation.Phase, u.Invitation.AcceptedAt, u.Invitation.AcceptedVia = InvitationAccepted, new(now), new(intent.Action)
	u.Status = StatusActive
	return nil
}

// CheckInvitationRoles validates all names and only the authority newly added.
// Retaining or removing an existing role never requires granting it again.
func CheckInvitationRoles(was, want []string, access InvitationAccess) error {
	for _, role := range CanonicalRoles(want) {
		if !roleName.MatchString(role) || !slices.Contains(access.KnownRoles, role) {
			return fmt.Errorf("%w: unknown or invalid role", crud.ErrInvalid)
		}
		if slices.Contains(was, role) {
			continue
		}
		if !slices.Contains(access.GrantableRoles, role) || (slices.Contains(access.Administering, role) && !access.GrantAdmin) {
			return ErrInvitationForbidden
		}
	}
	return nil
}

// CheckInvitationAdministration applies the existing floor without an audit write.
// A waiting appointment protects the final way back, never an active successor.
func CheckInvitationAdministration(before, after *User, administering []string, others []*User) error {
	if !before.Administers(administering) || after.Administers(administering) {
		return nil
	}
	last := !slices.ContainsFunc(others, func(u *User) bool { return u.Administers(administering) })
	return CheckedAdministration(UserReach(before, administering, others), UserReach(after, administering, others), last, LeavingAdministration(before, administering))
}
