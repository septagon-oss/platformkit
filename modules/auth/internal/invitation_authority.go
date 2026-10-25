package internal

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"gorm.io/gorm/clause"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	user "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// InspectInvitationAuthority resolves grants from the fresh actor and locks a
// scoped credential before deciding which role additions it can authorize.
func (s *Service) InspectInvitationAuthority(ctx context.Context, tx db.Tx[db.Tenant], p tenancy.Principal, actor *user.User) (user.InvitationAccess, error) {
	if actor == nil || actor.ID != p.UserID || actor.Status != user.StatusActive || actor.DeletedAt != nil {
		return user.InvitationAccess{}, user.ErrInvitationForbidden
	}
	held, err := s.Permissions(ctx, tx, actor.Roles)
	if err != nil {
		return user.InvitationAccess{}, err
	}
	declared := s.declared()
	if p.TokenID != uuid.Nil {
		var key apiTokenRow
		if err := tx.DB().Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND user_id = ?", p.TokenID, actor.ID).Take(&key).Error; err != nil {
			if errors.Is(crud.Classify(err), crud.ErrNotFound) {
				return user.InvitationAccess{}, user.ErrInvitationUnauthenticated
			}
			return user.InvitationAccess{}, err
		}
		if key.RevokedAt != nil || !s.invitationNow().Before(key.ExpiresAt) {
			return user.InvitationAccess{}, user.ErrInvitationUnauthenticated
		}
		scoped := make(contracts.Permissions, 0, len(key.Scopes))
		for _, scope := range key.Scopes {
			if contracts.Grants(held, grantFor(declared, scope)) {
				scoped = append(scoped, scope)
			}
		}
		held = scoped
	} else if p.Permissions != nil {
		scoped := make(contracts.Permissions, 0, len(p.Permissions))
		for _, scope := range p.Permissions {
			if contracts.Grants(held, grantFor(declared, scope)) {
				scoped = append(scoped, scope)
			}
		}
		held = scoped
	}
	access := user.InvitationAccess{Read: contracts.Grants(held, grantFor(declared, user.PermissionUserRead)),
		Manage:     contracts.Grants(held, grantFor(declared, user.PermissionUserManage)),
		GrantAdmin: contracts.Grants(held, grantFor(declared, contracts.PermissionRoleManage))}
	roles, err := s.Roles(ctx, tx)
	if err != nil {
		return user.InvitationAccess{}, err
	}
	for _, role := range roles {
		access.KnownRoles = append(access.KnownRoles, role.Name)
		administers := contracts.Grants(role.Grants, grantFor(declared, contracts.PermissionRoleManage))
		if administers {
			access.Administering = append(access.Administering, role.Name)
		}
		grantable := (!administers || access.GrantAdmin) && invitationGrantsSubset(role.Grants, held, declared, db.TenantOf(tx).Operator)
		if grantable {
			access.GrantableRoles = append(access.GrantableRoles, role.Name)
		}
	}
	return access, nil
}

func invitationGrantsSubset(want, held []string, declared []tenancy.Grant, operator bool) bool {
	for _, permission := range want {
		grant := grantFor(declared, permission)
		if (grant.Operator && !operator) || !contracts.Grants(held, grant) {
			return false
		}
	}
	return true
}

// GetInvitationReading projects one authorized snapshot.
func (s *Service) GetInvitationReading(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*contracts.InvitationReading, error) {
	if s.invitationReader == nil {
		return nil, user.ErrInvitationUnavailable
	}
	u, err := s.invitationReader.GetInvitation(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	return s.invitationReading(u), nil
}

// ListInvitationReadings returns no partial page when a snapshot cannot be read.
func (s *Service) ListInvitationReadings(ctx context.Context, tx db.Tx[db.Tenant], query user.InvitationQuery) ([]*contracts.InvitationReading, error) {
	if s.invitationReader == nil {
		return nil, user.ErrInvitationUnavailable
	}
	rows, err := s.invitationReader.ListInvitations(ctx, tx, query)
	if err != nil {
		return nil, err
	}
	out := make([]*contracts.InvitationReading, 0, len(rows))
	for _, row := range rows {
		reading := s.invitationReading(row)
		out = append(out, reading)
	}
	return out, nil
}

// invitationReading separates what this invitation recorded from what a transport
// reported: this build carries no durable mail-attempt record, so the committed
// intent is answered from the snapshot and the observation is never more than
// "no evidence" — never a claimed delivery.
func (s *Service) invitationReading(row *user.InvitationSnapshot) *contracts.InvitationReading {
	r := &contracts.InvitationReading{InvitationSnapshot: row, Request: "queued", Observation: "unobserved"}
	if row.DeliveryID == nil {
		r.Request, r.Observation = "unrecorded", "unknown"
	}
	if row.State != user.InvitationPending {
		r.Request = "ended"
	}
	return r
}
