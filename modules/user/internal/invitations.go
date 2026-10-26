package internal

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	auth "github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/user/contracts"
)

type invitationClock struct{}

func (invitationClock) Now() time.Time { return db.Now() }

// ConfigureInvitations supplies the declared capability before serving requests.
// Nil clock uses the database clock's UTC microsecond convention.
func (s *Service) ConfigureInvitations(clock contracts.Clock, timing contracts.InvitationTiming, authority contracts.InvitationAuthority) {
	if clock == nil {
		clock = invitationClock{}
	}
	s.clock, s.invitationTiming, s.invitationAuthority = clock, timing, authority
}

var (
	_ contracts.InvitationCommands  = (*Service)(nil)
	_ contracts.InvitationReader    = (*Service)(nil)
	_ contracts.InvitationRetention = (*Service)(nil)
)

func (s *Service) invitationNow() time.Time {
	if s.clock == nil {
		return db.Now()
	}
	return s.clock.Now().UTC().Truncate(time.Microsecond)
}

func (s *Service) invitationAccess(ctx context.Context, tx db.Tx[db.Tenant], manage bool) (contracts.InvitationAccess, error) {
	p, ok := tenancy.PrincipalFrom(ctx)
	if !ok || p.UserID == uuid.Nil {
		return contracts.InvitationAccess{}, contracts.ErrInvitationUnauthenticated
	}
	if s.invitationAuthority == nil || s.invitationTiming.Lifetime <= 0 || s.invitationTiming.ResendInterval <= 0 {
		return contracts.InvitationAccess{}, contracts.ErrInvitationUnavailable
	}
	if err := s.lockAdministration(ctx, tx); err != nil {
		return contracts.InvitationAccess{}, err
	}
	actor, err := crud.Get[*contracts.User](tx, p.UserID)
	if err != nil || actor.Status != contracts.StatusActive {
		return contracts.InvitationAccess{}, contracts.ErrInvitationForbidden
	}
	access, err := s.invitationAuthority.InspectInvitationAuthority(ctx, tx, p, actor)
	if err != nil {
		if errors.Is(err, contracts.ErrInvitationUnauthenticated) || errors.Is(err, contracts.ErrInvitationForbidden) {
			return contracts.InvitationAccess{}, err
		}
		return contracts.InvitationAccess{}, fmt.Errorf("%w: authority lookup failed", contracts.ErrInvitationUnavailable)
	}
	if (manage && !access.Manage) || (!manage && !access.Read) {
		return contracts.InvitationAccess{}, contracts.ErrInvitationForbidden
	}
	return access, nil
}

func (s *Service) lockAdministration(ctx context.Context, tx db.Tx[db.Tenant]) error {
	return tx.DB().WithContext(ctx).Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", administrationLock(db.TenantOf(tx))).Error
}

// CreateInvitation checks all role additions before inserting the tenant person.
func (s *Service) CreateInvitation(ctx context.Context, tx db.Tx[db.Tenant], intent contracts.InvitationIntent) (*contracts.User, error) {
	access, err := s.invitationAccess(ctx, tx, true)
	if err != nil {
		return nil, err
	}
	if err := contracts.CheckInvitationRoles(nil, intent.Roles, access); err != nil {
		return nil, err
	}
	return s.createManagedInvitation(ctx, tx, intent)
}

func (s *Service) createManagedInvitation(ctx context.Context, tx db.Tx[db.Tenant], intent contracts.InvitationIntent) (*contracts.User, error) {
	u, at, err := s.newManagedInvitation(ctx, tx, intent)
	if err != nil {
		return nil, err
	}
	write := tenantEvents(tx)
	if err := s.publishInvitation(ctx, write, u, contracts.EventInvited, at); err != nil {
		return nil, err
	}
	if len(u.Roles) > 0 {
		if err := write(ctx, contracts.EventRolesSet, contracts.RolesSet{UserID: u.ID, Now: u.Roles, At: at}); err != nil {
			return nil, err
		}
	}
	return u, nil
}

// newManagedInvitation writes the person and the first record of what was asked for
// them, and publishes nothing: the caller writes the facts through the outbox door
// its own transaction holds — tenantEvents at a tenant edge, systemEvents across it.
func (s *Service) newManagedInvitation(ctx context.Context, tx db.Tx[db.Tenant], intent contracts.InvitationIntent) (*contracts.User, time.Time, error) {
	u := &contracts.User{Email: intent.Email, DisplayName: intent.DisplayName, Roles: contracts.CanonicalRoles(intent.Roles), Status: contracts.StatusInvited}
	if err := u.Validate(ctx); err != nil {
		return nil, time.Time{}, fmt.Errorf("%w: %s", crud.ErrInvalid, err)
	}
	at := s.invitationNow()
	h, err := contracts.BeginInvitation(u, s.invitationTiming, at, uuid.New(), false)
	if err != nil {
		return nil, time.Time{}, err
	}
	u.Invitation = h
	if err := crud.Create(ctx, tx, u); err != nil {
		return nil, at, err
	}
	return u, at, nil
}

// provisionWaitingInvitation is the control plane's cross-tenant first half: the
// person and their recorded invitation, in the named tenant, from a transaction that
// belongs to none. The row is written inside the bridged scope and its facts on the
// system transaction, because an event for a tenant the request's operation did not
// run in is refused at the tenant's own outbox door (kit/events/operation.go) and
// events.PublishFor is the door this transaction holds. SucceedFor marks the
// operation the password-carrying path marks it, so the trail reads the same.
func (s *Service) provisionWaitingInvitation(ctx context.Context, tx db.Tx[db.System], tenantID uuid.UUID,
	email, displayName string, roles []string,
) (*contracts.User, error) {
	var (
		u  *contracts.User
		at time.Time
	)
	if err := db.InTenant(ctx, tx, tenancy.Tenant{ID: tenantID}, func(ctx context.Context, scoped db.Tx[db.Tenant]) error {
		var err error
		u, at, err = s.newManagedInvitation(ctx, scoped, contracts.InvitationIntent{Email: email, DisplayName: displayName, Roles: roles})
		return err
	}); err != nil {
		return nil, err
	}
	if err := events.SucceedFor(ctx, tx, at); err != nil {
		return nil, err
	}
	write := systemEvents(tx, tenantID)
	if err := s.publishInvitation(ctx, write, u, contracts.EventInvited, at); err != nil {
		return nil, err
	}
	if len(u.Roles) > 0 {
		if err := write(ctx, contracts.EventRolesSet, contracts.RolesSet{UserID: u.ID, Now: u.Roles, At: at}); err != nil {
			return nil, err
		}
	}
	return u, nil
}

// eventWriter is one door onto the outbox: the tenant's own Publish, or the system
// transaction's PublishFor for a tenant named from outside it.
type eventWriter func(context.Context, string, any) error

func tenantEvents(tx db.Tx[db.Tenant]) eventWriter {
	return func(ctx context.Context, name string, payload any) error {
		return events.Publish(ctx, tx, name, payload)
	}
}

func systemEvents(tx db.Tx[db.System], tenantID uuid.UUID) eventWriter {
	return func(ctx context.Context, name string, payload any) error {
		return events.PublishFor(ctx, tx, tenantID, name, payload)
	}
}

// ResendInvitation replaces the credential generation at the command's commit.
func (s *Service) ResendInvitation(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, revision int64) (*contracts.InvitationSnapshot, error) {
	u, access, at, err := s.changeInvitation(ctx, tx, id, contracts.InvitationMutation{Action: contracts.InvitationResend, ExpectedRevision: revision, DeliveryID: uuid.New()})
	if err != nil {
		return nil, err
	}
	return contracts.ReadInvitation(u, access, s.invitationTiming, at), nil
}

// RevokeInvitation ends the invitation; a current-revision repeat is unchanged.
func (s *Service) RevokeInvitation(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, revision int64) (*contracts.InvitationSnapshot, error) {
	u, access, at, err := s.changeInvitation(ctx, tx, id, contracts.InvitationMutation{Action: contracts.InvitationRevoke, ExpectedRevision: revision})
	if err != nil {
		return nil, err
	}
	return contracts.ReadInvitation(u, access, s.invitationTiming, at), nil
}

// SetInvitationRoles changes only roles and revision, leaving the mail request intact.
func (s *Service) SetInvitationRoles(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, revision int64, roles []string) (*contracts.User, error) {
	u, _, _, err := s.changeInvitation(ctx, tx, id, contracts.InvitationMutation{Action: contracts.InvitationRoles, ExpectedRevision: revision, Roles: roles})
	return u, err
}

func (s *Service) changeInvitation(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, intent contracts.InvitationMutation) (*contracts.User, contracts.InvitationAccess, time.Time, error) {
	u, err := crud.GetForUpdate[*contracts.User](tx, id)
	if err != nil {
		return nil, contracts.InvitationAccess{}, time.Time{}, err
	}
	access, err := s.invitationAccess(ctx, tx, true)
	if err != nil {
		return nil, contracts.InvitationAccess{}, time.Time{}, err
	}
	at := s.invitationNow()
	if err := s.adoptInvitation(u, at); err != nil {
		return nil, access, at, err
	}
	next, changed, err := contracts.DecideInvitation(u, intent, s.invitationTiming, at)
	if err != nil {
		return nil, access, at, err
	}
	if intent.Action == contracts.InvitationRoles {
		if err := contracts.CheckInvitationRoles(u.Roles, next.Roles, access); err != nil {
			return nil, access, at, err
		}
	}
	if !changed {
		return next, access, at, nil
	}
	if _, _, err := s.invitationFloor(ctx, tx, u, next); err != nil {
		return nil, access, at, err
	}
	if err := s.saveInvitation(ctx, tx, u, next, intent.Action, at); err != nil {
		return nil, access, at, err
	}
	return next, access, at, nil
}

func (s *Service) adoptInvitation(u *contracts.User, at time.Time) error {
	if u.Status != contracts.StatusInvited || u.Invitation.Phase != "" {
		return nil
	}
	h, err := contracts.BeginInvitation(u, s.invitationTiming, at, uuid.Nil, true)
	if err == nil {
		u.Invitation = h
	}
	return err
}

// invitationFloor decides the last-administrator floor about a person whose row
// carries invitation history: the tenant's administration lock, the roles the
// application names as administering, and every other person in the tenant who could
// still hold one — a delivered invitation is a way back a count of active people
// alone cannot see. It writes nothing.
//
// It answers the roles it counted and whether it was the rule that refused, because
// the two doors that end a person's holding — the delete hook and the erasure — record
// the refusal the way floor does, with those roles as the grant it would have taken.
// A failed read is not a refusal and is answered as refused=false: recording one would
// put a line on the trail about a refusal the rule never made, naming no grant.
// Invitation commands are refused by a rule that writes nothing and emits nothing, so
// their callers use neither answer.
func (s *Service) invitationFloor(ctx context.Context, tx db.Tx[db.Tenant], before, after *contracts.User) (roles []string, refused bool, err error) {
	if err := s.lockAdministration(ctx, tx); err != nil {
		return nil, false, err
	}
	roles, err = s.administering.Administering(ctx, tx)
	if err != nil {
		return nil, false, err
	}
	if !before.Administers(roles) || after.Administers(roles) {
		return roles, false, nil
	}
	var others []*contracts.User
	if err := tx.DB().Where("id <> ? AND deleted_at IS NULL AND status <> ?", before.ID, contracts.StatusInactive).Find(&others).Error; err != nil {
		return roles, false, err
	}
	err = contracts.CheckInvitationAdministration(before, after, roles, others)
	return roles, err != nil, err
}

func (s *Service) saveInvitation(ctx context.Context, tx db.Tx[db.Tenant], before, next *contracts.User, action string, at time.Time) error {
	columns := []string{"status", "roles", "updated_at"}
	for _, field := range []string{"phase", "revision", "generation", "recipient", "adopted_at", "requested_at", "expires_at", "delivery_id", "accepted_at", "accepted_via", "revoked_at"} {
		columns = append(columns, "invitation_"+field)
	}
	if err := crud.Update(ctx, tx, next, columns...); err != nil {
		return err
	}
	if !slices.Equal(before.Roles, next.Roles) {
		if err := events.Publish(ctx, tx, contracts.EventRolesSet, contracts.RolesSet{UserID: next.ID, Was: before.Roles, Now: next.Roles, At: at}); err != nil {
			return err
		}
	}
	publish := tenantEvents(tx)
	switch action {
	case contracts.InvitationResend:
		return s.publishInvitation(ctx, publish, next, contracts.EventInvitationResent, at)
	case contracts.InvitationRevoke:
		return s.publishInvitation(ctx, publish, next, contracts.EventInvitationRevoked, at)
	case contracts.InvitationPasswordLink, contracts.InvitationDirectPassword, contracts.InvitationIdentityProvider:
		return s.publishInvitation(ctx, publish, next, contracts.EventInvitationAccepted, at)
	}
	return nil
}

// publishInvitation writes the one fact an invitation change left behind, in the
// payload the two modules declared, through whichever outbox door the caller holds.
// It is spelled once so a command inside a tenant and the control plane's
// cross-tenant write carry the same fields: auth's subscriber hands a link over only
// for the generation and delivery identity a committed event names, so neither door
// can leave a worker an event it would have to invent a send for.
func (s *Service) publishInvitation(ctx context.Context, write eventWriter, u *contracts.User, event string, at time.Time) error {
	if event == contracts.EventInvited {
		return write(ctx, event, contracts.Invited{UserID: u.ID, Email: u.Email, Status: u.Status, At: at,
			Served: httpx.ServedFrom(ctx), Generation: u.Invitation.Generation, Revision: u.Invitation.Revision, DeliveryID: u.Invitation.DeliveryID})
	}
	return write(ctx, event, contracts.InvitationTransition{UserID: u.ID, Generation: u.Invitation.Generation,
		Revision: u.Invitation.Revision, DeliveryID: u.Invitation.DeliveryID, At: at, AcceptedVia: u.Invitation.AcceptedVia, Served: httpx.ServedFrom(ctx)})
}

func (s *Service) setManagedRoles(ctx context.Context, tx db.Tx[db.Tenant], u *contracts.User, roles []string) (*contracts.User, error) {
	at := s.invitationNow()
	next, changed, err := contracts.DecideInvitation(u, contracts.InvitationMutation{Action: contracts.InvitationRoles, ExpectedRevision: u.Invitation.Revision, Roles: roles}, s.invitationTiming, at)
	if err != nil {
		return nil, err
	}
	if !changed {
		return next, nil
	}
	if _, _, err := s.invitationFloor(ctx, tx, u, next); err != nil {
		return nil, err
	}
	if err := s.saveInvitation(ctx, tx, u, next, contracts.InvitationRoles, at); err != nil {
		return nil, err
	}
	return next, nil
}

// CreatePerson gives generated and dedicated creation the same owner decision.
// Generic creation still refuses role and status shortcuts.
func (s *Service) CreatePerson(ctx context.Context, tx db.Tx[db.Tenant], input *contracts.User) (*contracts.User, error) {
	if input == nil || len(input.Roles) > 0 || (input.Status != "" && input.Status != contracts.StatusInvited) {
		return nil, fmt.Errorf("%w: the collection creates invitations; roles belong to their command", crud.ErrInvalid)
	}
	u, err := s.CreateInvitation(ctx, tx, contracts.InvitationIntent{Email: input.Email, DisplayName: input.DisplayName})
	if err != nil {
		return nil, InvitationFault(err)
	}
	if input.Handle != "" {
		u, err = s.SetHandle(ctx, tx, u.ID, input.Handle)
		if err != nil {
			return nil, err
		}
	}
	if err := events.Publish(ctx, tx, contracts.EventCreated, u); err != nil {
		return nil, err
	}
	return u, nil
}

var _ auth.InvitationUsers = (*Service)(nil)

// RequestInvitationLink applies the same replacement decision to a trusted
// recovery event. Old, duplicate and terminal requests are acknowledged quietly.
func (s *Service) RequestInvitationLink(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, source auth.InvitationSource) error {
	if source.At.IsZero() || source.ID == uuid.Nil {
		return contracts.ErrInvitationUnavailable
	}
	if source.Kind != auth.InvitationRecoveryRequest && source.Kind != auth.InvitationLegacyRequest {
		return crud.ErrInvalid
	}
	u, err := s.LockInvitation(ctx, tx, id)
	if errors.Is(err, crud.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	at := s.invitationNow()
	next, changed, err := contracts.DecideInvitationRequest(u, source.Kind, source.At, uuid.New(), s.invitationTiming, at)
	if err != nil || !changed {
		return err
	}
	return s.saveInvitation(ctx, tx, u, next, contracts.InvitationResend, at)
}

// SetInvitationPassword records direct administrative admission at its revision.
func (s *Service) SetInvitationPassword(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, revision int64, password string) error {
	u, err := crud.GetForUpdate[*contracts.User](tx, id)
	if err != nil {
		return err
	}
	if _, err := s.invitationAccess(ctx, tx, true); err != nil {
		return err
	}
	at := s.invitationNow()
	if err := s.adoptInvitation(u, at); err != nil {
		return err
	}
	return s.acceptInvitationPassword(ctx, tx, u, contracts.InvitationMutation{Action: contracts.InvitationDirectPassword, ExpectedRevision: revision}, password, at)
}

// AcceptInvitationPassword is auth's trusted integration after its token checks.
// The same locked decision instant governs token and invitation expiry.
func (s *Service) AcceptInvitationPassword(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, generation int64, password string, at time.Time) error {
	u, err := crud.GetForUpdate[*contracts.User](tx, id)
	if err != nil {
		return err
	}
	if err := s.adoptInvitation(u, at); err != nil {
		return err
	}
	return s.acceptInvitationPassword(ctx, tx, u, contracts.InvitationMutation{Action: contracts.InvitationPasswordLink, Generation: generation}, password, at)
}

func (s *Service) acceptInvitationPassword(ctx context.Context, tx db.Tx[db.Tenant], u *contracts.User, intent contracts.InvitationMutation, password string, at time.Time) error {
	next, _, err := contracts.DecideInvitation(u, intent, s.invitationTiming, at)
	if err != nil {
		return err
	}
	hash, err := contracts.HashPassword(password)
	if err != nil {
		return fmt.Errorf("%w: %s", crud.ErrInvalid, err)
	}
	if err := s.saveInvitation(ctx, tx, u, next, intent.Action, at); err != nil {
		return err
	}
	next.PasswordHash = hash
	if err := crud.Update(ctx, tx, next, "password_hash", "updated_at"); err != nil {
		return err
	}
	return events.Publish(ctx, tx, contracts.EventPasswordSet, contracts.PasswordSet{UserID: next.ID, At: at})
}

// LockInvitation locks the aggregate root and adopts legacy history once when
// timing is configured. Custom legacy compositions retain their prior behavior.
func (s *Service) LockInvitation(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*contracts.User, error) {
	u, err := crud.GetForUpdate[*contracts.User](tx, id)
	if err != nil {
		return nil, err
	}
	if u.Status == contracts.StatusInvited && u.Invitation.Phase == "" && s.invitationTiming.Lifetime > 0 {
		if err := s.adoptInvitation(u, s.invitationNow()); err != nil {
			return nil, err
		}
		if err := crud.Update(ctx, tx, u, "invitation_phase", "invitation_revision", "invitation_recipient", "invitation_adopted_at", "invitation_expires_at", "updated_at"); err != nil {
			return nil, err
		}
	}
	return u, nil
}
