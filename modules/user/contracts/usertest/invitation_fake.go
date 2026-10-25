package usertest

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	auth "github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/user/contracts"
)

// ConfigureInvitations enables managed invitations using the SQL owner's inputs.
func (f *Fake) ConfigureInvitations(clock contracts.Clock, timing contracts.InvitationTiming, authority contracts.InvitationAuthority) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.invitationClock, f.invitationTiming, f.invitationAuthority = clock, timing, authority
}

func (f *Fake) invitationNow() time.Time {
	if f.invitationClock == nil {
		return db.Now()
	}
	return f.invitationClock.Now().UTC().Truncate(time.Microsecond)
}

func (f *Fake) invitationAccess(ctx context.Context, tx db.Tx[db.Tenant], manage bool) (contracts.InvitationAccess, error) {
	p, ok := tenancy.PrincipalFrom(ctx)
	if !ok || p.UserID == uuid.Nil {
		return contracts.InvitationAccess{}, contracts.ErrInvitationUnauthenticated
	}
	if f.invitationAuthority == nil || f.invitationTiming.Lifetime <= 0 || f.invitationTiming.ResendInterval <= 0 {
		return contracts.InvitationAccess{}, contracts.ErrInvitationUnavailable
	}
	u, err := f.invitationSubject(ctx, tx, p.UserID)
	if err != nil || u.Status != contracts.StatusActive {
		return contracts.InvitationAccess{}, contracts.ErrInvitationForbidden
	}
	access, err := f.invitationAuthority.InspectInvitationAuthority(ctx, tx, p, u)
	if err != nil {
		if errors.Is(err, contracts.ErrInvitationUnauthenticated) || errors.Is(err, contracts.ErrInvitationForbidden) {
			return contracts.InvitationAccess{}, err
		}
		return contracts.InvitationAccess{}, contracts.ErrInvitationUnavailable
	}
	if (manage && !access.Manage) || (!manage && !access.Read) {
		return contracts.InvitationAccess{}, contracts.ErrInvitationForbidden
	}
	return access, nil
}

func invitationTenant(ctx context.Context, tx db.Tx[db.Tenant]) uuid.UUID {
	if id := db.TenantOf(tx).ID; id != uuid.Nil {
		return id
	}
	t, _ := tenancy.FromContext(ctx)
	return t.ID
}

func (f *Fake) invitationSubject(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*contracts.User, error) {
	u, err := f.get(id)
	if err != nil {
		return nil, err
	}
	if u.TenantID != invitationTenant(ctx, tx) {
		return nil, crud.ErrNotFound
	}
	return u, nil
}

func (f *Fake) CreateInvitation(ctx context.Context, tx db.Tx[db.Tenant], intent contracts.InvitationIntent) (*contracts.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	access, err := f.invitationAccess(ctx, tx, true)
	if err != nil {
		return nil, err
	}
	if err := contracts.CheckInvitationRoles(nil, intent.Roles, access); err != nil {
		return nil, err
	}
	u := &contracts.User{Email: intent.Email, DisplayName: intent.DisplayName, Roles: contracts.CanonicalRoles(intent.Roles), Status: contracts.StatusInvited}
	if err := u.Validate(ctx); err != nil {
		return nil, fmt.Errorf("%w: %s", crud.ErrInvalid, err)
	}
	u.TenantID = invitationTenant(ctx, tx)
	for _, row := range f.users {
		if row.TenantID == u.TenantID && row.DeletedAt == nil && row.Email == u.Email {
			return nil, crud.ErrConflict
		}
	}
	at := f.invitationNow()
	h, err := contracts.BeginInvitation(u, f.invitationTiming, at, uuid.New(), false)
	if err != nil {
		return nil, err
	}
	u.ID, u.CreatedAt, u.UpdatedAt, u.Invitation = uuid.New(), at, at, h
	f.users[u.ID] = *u
	f.published = append(f.published, contracts.EventInvited)
	if len(u.Roles) > 0 {
		f.published = append(f.published, contracts.EventRolesSet)
	}
	return f.get(u.ID)
}

func (f *Fake) ResendInvitation(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, revision int64) (*contracts.InvitationSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, access, err := f.changeInvitation(ctx, tx, id, contracts.InvitationMutation{Action: contracts.InvitationResend, ExpectedRevision: revision, DeliveryID: uuid.New()}, "")
	if err != nil {
		return nil, err
	}
	return contracts.ReadInvitation(u, access, f.invitationTiming, f.invitationNow()), nil
}

func (f *Fake) RevokeInvitation(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, revision int64) (*contracts.InvitationSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, access, err := f.changeInvitation(ctx, tx, id, contracts.InvitationMutation{Action: contracts.InvitationRevoke, ExpectedRevision: revision}, "")
	if err != nil {
		return nil, err
	}
	return contracts.ReadInvitation(u, access, f.invitationTiming, f.invitationNow()), nil
}

func (f *Fake) SetInvitationRoles(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, revision int64, roles []string) (*contracts.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, _, err := f.changeInvitation(ctx, tx, id, contracts.InvitationMutation{Action: contracts.InvitationRoles, ExpectedRevision: revision, Roles: roles}, "")
	return u, err
}

func (f *Fake) SetInvitationPassword(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, revision int64, password string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, _, err := f.changeInvitation(ctx, tx, id, contracts.InvitationMutation{Action: contracts.InvitationDirectPassword, ExpectedRevision: revision}, password)
	return err
}

func (f *Fake) changeInvitation(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, intent contracts.InvitationMutation, password string) (*contracts.User, contracts.InvitationAccess, error) {
	access, err := f.invitationAccess(ctx, tx, true)
	if err != nil {
		return nil, access, err
	}
	u, err := f.invitationSubject(ctx, tx, id)
	if err != nil {
		return nil, access, err
	}
	at := f.invitationNow()
	if err := f.adoptInvitation(u, at); err != nil {
		return nil, access, err
	}
	next, changed, err := contracts.DecideInvitation(u, intent, f.invitationTiming, at)
	if err != nil {
		return nil, access, err
	}
	if intent.Action == contracts.InvitationRoles {
		if err := contracts.CheckInvitationRoles(u.Roles, next.Roles, access); err != nil {
			return nil, access, err
		}
	}
	if !changed {
		return next, access, nil
	}
	if err := f.invitationFloor(u, next); err != nil {
		return nil, access, err
	}
	if intent.Action == contracts.InvitationDirectPassword {
		hash, err := contracts.HashPassword(password)
		if err != nil {
			return nil, access, fmt.Errorf("%w: %s", crud.ErrInvalid, err)
		}
		next.PasswordHash = hash
	}
	f.storeInvitation(u, next, intent.Action, at)
	got, err := f.get(id)
	return got, access, err
}

func (f *Fake) storeInvitation(before, next *contracts.User, action string, at time.Time) {
	next.UpdatedAt = at
	f.users[next.ID] = *next
	if !slices.Equal(before.Roles, next.Roles) {
		f.published = append(f.published, contracts.EventRolesSet)
	}
	switch action {
	case contracts.InvitationResend:
		f.published = append(f.published, contracts.EventInvitationResent)
	case contracts.InvitationRevoke:
		f.published = append(f.published, contracts.EventInvitationRevoked)
	case contracts.InvitationDirectPassword, contracts.InvitationPasswordLink:
		f.published = append(f.published, contracts.EventInvitationAccepted, contracts.EventPasswordSet)
	case contracts.InvitationIdentityProvider:
		f.published = append(f.published, contracts.EventInvitationAccepted)
	}
}

func (f *Fake) invitationFloor(before, after *contracts.User) error {
	var others []*contracts.User
	for _, other := range f.users {
		if other.ID != before.ID && other.TenantID == before.TenantID {
			others = append(others, &other)
		}
	}
	return contracts.CheckInvitationAdministration(before, after, f.administering, others)
}

func (f *Fake) GetInvitation(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*contracts.InvitationSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	access, err := f.invitationAccess(ctx, tx, false)
	if err != nil {
		return nil, err
	}
	u, err := f.invitationSubject(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if u.Invitation.Phase == "" {
		if u.Status == contracts.StatusInvited {
			return nil, contracts.ErrInvitationInitializing
		}
		return nil, crud.ErrNotFound
	}
	at := f.invitationNow()
	if err := contracts.CheckInvitationTime(u.Invitation, at); err != nil {
		return nil, err
	}
	return contracts.ReadInvitation(u, access, f.invitationTiming, at), nil
}

func (f *Fake) ListInvitations(ctx context.Context, tx db.Tx[db.Tenant], query contracts.InvitationQuery) ([]*contracts.InvitationSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	access, err := f.invitationAccess(ctx, tx, false)
	if err != nil {
		return nil, err
	}
	q, err := contracts.CheckInvitationQuery(query)
	if err != nil {
		return nil, err
	}
	at := f.invitationNow()
	var rows []*contracts.User
	for _, stored := range f.users {
		if stored.TenantID != invitationTenant(ctx, tx) || stored.DeletedAt != nil {
			continue
		}
		if stored.Invitation.Phase == "" {
			if stored.Status == contracts.StatusInvited {
				return nil, contracts.ErrInvitationInitializing
			}
			continue
		}
		if q.State != "" && contracts.InvitationState(stored.Invitation, at) != q.State {
			continue
		}
		u, _ := f.get(stored.ID)
		rows = append(rows, u)
	}
	slices.SortFunc(rows, func(a, b *contracts.User) int {
		if n := b.CreatedAt.Compare(a.CreatedAt); n != 0 {
			return n
		}
		return strings.Compare(a.ID.String(), b.ID.String())
	})
	out := make([]*contracts.InvitationSnapshot, 0)
	for _, u := range rows[min(q.Offset, len(rows)):min(q.Offset+q.Limit, len(rows))] {
		if err := contracts.CheckInvitationTime(u.Invitation, at); err != nil {
			return nil, err
		}
		out = append(out, contracts.ReadInvitation(u, access, f.invitationTiming, at))
	}
	return out, nil
}

// LockInvitation returns the tenant's detached snapshot; the fake serializes
// each command with its mutex and does not model transaction-held row locks.
func (f *Fake) LockInvitation(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*contracts.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, err := f.invitationSubject(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if u.Status == contracts.StatusInvited && u.Invitation.Phase == "" && f.invitationTiming.Lifetime > 0 {
		if err := f.adoptInvitation(u, f.invitationNow()); err != nil {
			return nil, err
		}
		f.users[id] = *u
	}
	return u, nil
}

// AcceptInvitationPassword uses the same generation and expiry decision as SQL.
func (f *Fake) AcceptInvitationPassword(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, generation int64, password string, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, err := f.invitationSubject(ctx, tx, id)
	if err != nil {
		return err
	}
	return f.acceptInvitationPassword(u, contracts.InvitationMutation{Action: contracts.InvitationPasswordLink, Generation: generation}, password, at)
}

func (f *Fake) acceptInvitationPassword(u *contracts.User, intent contracts.InvitationMutation, password string, at time.Time) error {
	if err := f.adoptInvitation(u, at); err != nil {
		return err
	}
	next, _, err := contracts.DecideInvitation(u, intent, f.invitationTiming, at)
	if err != nil {
		return err
	}
	hash, err := contracts.HashPassword(password)
	if err != nil {
		return fmt.Errorf("%w: %s", crud.ErrInvalid, err)
	}
	next.PasswordHash = hash
	f.storeInvitation(u, next, intent.Action, at)
	return nil
}

// RequestInvitationLink applies the shared original-request fence and cooldown.
func (f *Fake) RequestInvitationLink(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, source auth.InvitationSource) error {
	if source.ID == uuid.Nil || source.At.IsZero() {
		return contracts.ErrInvitationUnavailable
	}
	if source.Kind != auth.InvitationRecoveryRequest && source.Kind != auth.InvitationLegacyRequest {
		return crud.ErrInvalid
	}
	if _, err := f.LockInvitation(ctx, tx, id); err != nil {
		if errors.Is(err, crud.ErrNotFound) {
			return nil
		}
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	u, err := f.invitationSubject(ctx, tx, id)
	if err != nil {
		return err
	}
	at := f.invitationNow()
	next, changed, err := contracts.DecideInvitationRequest(u, source.Kind, source.At, uuid.New(), f.invitationTiming, at)
	if err == nil && changed {
		f.storeInvitation(u, next, contracts.InvitationResend, at)
	}
	return err
}

func (f *Fake) adoptInvitation(u *contracts.User, at time.Time) error {
	if u.Status != contracts.StatusInvited || u.Invitation.Phase != "" {
		return nil
	}
	h, err := contracts.BeginInvitation(u, f.invitationTiming, at, uuid.Nil, true)
	if err == nil {
		u.Invitation = h
	}
	return err
}

// LockForErasure returns retained history and refuses a recorded invitation that is
// still open, the way the SQL owner does: an unrecorded row has no revision to
// withdraw. The fake enforces the shared floor but has no transaction-held lock
// lifetime.
func (f *Fake) LockForErasure(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*contracts.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.users[id]
	if !ok || u.TenantID != invitationTenant(ctx, tx) {
		return nil, crud.ErrNotFound
	}
	if u.Invitation.Phase == contracts.InvitationPending {
		return nil, crud.ErrConflict
	}
	if err := f.invitationFloor(&u, nil); err != nil {
		return nil, err
	}
	u.Invitation = u.Invitation.Clone()
	u.Roles = contracts.CanonicalRoles(u.Roles)
	return &u, nil
}
