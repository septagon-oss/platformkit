package internal

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/scim/contracts"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// userNameFilter is the whole of this service's filter grammar: one comparison,
// on the one attribute a connector actually filters on. Anything else refuses
// the request rather than answering it unfiltered, because a connector that
// asked for a subset and was handed the directory would go on to act on
// everybody it meant to leave alone.
func userNameFilter(filter string) (value string, ok bool, err error) {
	f := strings.TrimSpace(filter)
	if f == "" {
		return "", true, nil
	}
	fields := strings.Fields(f)
	if len(fields) != 3 || !strings.EqualFold(fields[1], "eq") {
		return "", false, fmt.Errorf("%w: %q; this service compares userName with eq and nothing else",
			crud.ErrInvalid, filter)
	}
	if !strings.EqualFold(fields[0], "userName") {
		return "", false, fmt.Errorf("%w: %q; userName is the only attribute this service filters on",
			crud.ErrInvalid, filter)
	}
	value = strings.Trim(fields[2], `"`)
	if value == "" {
		return "", false, fmt.Errorf("%w: userName eq was given nothing", crud.ErrInvalid)
	}
	return value, false, nil
}

// ListUsers pages the tenant's people.
func (s *Service) ListUsers(_ context.Context, tx db.Tx[db.Tenant], q contracts.ListQuery) (*contracts.UserList, error) {
	want, _, err := userNameFilter(q.Filter)
	if err != nil {
		return nil, err
	}
	limit, offset := pageBounds(q)
	db_ := tx.DB().Table("users u").
		// A soft-deleted person is not here: the row keeps its address reserved so
		// that the same person is never provisioned twice, and it stays out of
		// every list a connector may act on.
		Where("u.tenant_id = ? AND u.deleted_at IS NULL", db.TenantOf(tx).ID)
	if want != "" {
		db_ = db_.Where("lower(u.email) = lower(?)", want)
	}
	var total int64
	if err := db_.Count(&total).Error; err != nil {
		return nil, fmt.Errorf("scim: count people: %w", err)
	}
	var rows []userRow
	if err := db_.Select("u.id, u.email, u.display_name, u.status, u.created_at, u.updated_at, u.deleted_at, e.external_id").
		Joins("LEFT JOIN scim_user_external e ON e.user_id = u.id").
		Order("u.created_at DESC, u.id").Limit(limit).Offset(offset).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("scim: list people: %w", err)
	}
	out := &contracts.UserList{
		Schemas: []string{contracts.SchemaListResponse}, TotalResults: int(total),
		StartIndex: offset + 1, ItemsPerPage: limit,
	}
	for _, row := range rows {
		groups, err := groupsOf(tx, row.ID)
		if err != nil {
			return nil, err
		}
		out.Resources = append(out.Resources, row.resource(groups))
	}
	return out, nil
}

// groupsOf is the read-only half of membership: which of the provider's
// collections this person is in. It is a query and not a column because the
// membership lives in one table with one door — the Groups one. It is a function
// and not a method because it consults nothing a Service holds: the module's
// personal-data part (internal/personaldata.go) asks the same question of the same
// two tables, and one spelling of it is the point.
func groupsOf(tx db.Tx[db.Tenant], userID uuid.UUID) ([]contracts.GroupRef, error) {
	var rows []struct {
		ID   uuid.UUID `gorm:"column:id"`
		Name string    `gorm:"column:name"`
	}
	err := tx.DB().Table("scim_groups g").Select("g.id, g.name").
		Joins("JOIN scim_group_members m ON m.group_id = g.id").
		Where("m.user_id = ? AND g.tenant_id = ?", userID, db.TenantOf(tx).ID).
		Order("lower(g.name), g.id").Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("scim: read membership: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	out := make([]contracts.GroupRef, 0, len(rows))
	for _, r := range rows {
		out = append(out, contracts.GroupRef{Value: r.ID.String(), Type: "DIRECT", Display: r.Name})
	}
	return out, nil
}

// GetUser is one person of this tenant.
func (s *Service) GetUser(_ context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*contracts.UserResource, error) {
	row, err := readUser(tx, id)
	if err != nil {
		return nil, err
	}
	groups, err := groupsOf(tx, id)
	if err != nil {
		return nil, err
	}
	return row.resource(groups), nil
}

// readUser reads the person and the identifier the provider calls them by, in
// one query. The join is a LEFT JOIN because most people have no external id,
// and a person with none is the ordinary case rather than a missing row.
func readUser(tx db.Tx[db.Tenant], id uuid.UUID) (userRow, error) {
	var row userRow
	err := tx.DB().Table("users u").
		Select("u.id, u.email, u.display_name, u.status, u.created_at, u.updated_at, u.deleted_at, e.external_id").
		Joins("LEFT JOIN scim_user_external e ON e.user_id = u.id").
		Where("u.id = ? AND u.tenant_id = ? AND u.deleted_at IS NULL", id, db.TenantOf(tx).ID).
		Take(&row).Error
	if err != nil {
		return userRow{}, crud.Classify(err)
	}
	return row, nil
}

// CreateUser brings a person into being through the two commands the user module
// already has, neither of which is this module's to invent.
//
// active false, or absent, invites them: status invited, no password, and
// user.invited published in this same transaction, which is the event the auth
// module answers by offering a password-setup credential. active true says
// something stronger — an authority outside this application, the tenant's own
// identity provider, has confirmed this mailbox — and ConfirmAddress is the
// command the user module writes for exactly that fact. Neither half sets a
// password: a person a provider created signs in through the tenant's own door,
// and an integration that could hand over a password would be a second door into
// an account the tenant did not choose the key for.
func (s *Service) CreateUser(ctx context.Context, tx db.Tx[db.Tenant], in contracts.UserDraft) (*contracts.UserResource, error) {
	if err := s.mayProvision(ctx, tx); err != nil {
		return nil, err
	}
	if err := refused(in); err != nil {
		return nil, err
	}
	address, err := addressOf(in)
	if err != nil {
		return nil, err
	}
	u, err := s.users.Invite(ctx, tx, address, contracts.FoldName(in.DisplayName, in.Name))
	if err != nil {
		return nil, err
	}
	if in.Active != nil && *in.Active {
		if u, err = s.users.ConfirmAddress(ctx, tx, u.ID, address); err != nil {
			return nil, err
		}
	}
	wrote, err := s.setExternal(ctx, tx, u.ID, in.ExternalID)
	if err != nil {
		return nil, err
	}
	if wrote {
		// The person's arrival is user.invited and their activation is
		// user.email_verified, but the identifier the provider calls them by is this
		// module's row and no event under a `user.` name could describe it, so the
		// create that carried one says so here as well.
		if err := s.recordPersonChanged(ctx, tx, u.ID, "externalId"); err != nil {
			return nil, err
		}
	}
	return s.GetUser(ctx, tx, u.ID)
}

// ReplaceUser is a PUT. SCIM's replace means "this document is the whole of it",
// so a field the draft leaves empty is a field the caller is not naming, and the
// one thing it may not be is a reason to deactivate somebody: a connector that
// sends a name and an address and nothing else has said nothing about whether
// this person still works here.
//
// The address is refused, not ignored. userName is users.email, users.email is
// the login lookup, and the user module owns no command that moves it — a SCIM
// write that renamed the address would be an account the person cannot find their
// way back into, with no event anywhere saying it happened.
//
// `active` is read in both directions. A document that says `true` is saying the
// provider vouches for this mailbox, which is ConfirmAddress's fact and the same
// judgement PATCH makes; answering it by leaving an invited person unable to sign
// in while returning 200 would be a door that tells a connector its write landed
// when it did not, and Okta's documented update path is a PUT.
func (s *Service) ReplaceUser(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, in contracts.UserDraft) (*contracts.UserResource, error) {
	if err := s.mayProvision(ctx, tx); err != nil {
		return nil, err
	}
	if err := refused(in); err != nil {
		return nil, err
	}
	row, err := readUser(tx, id)
	if err != nil {
		return nil, err
	}
	if want := strings.ToLower(strings.TrimSpace(userNameOf(in, row))); want != "" && want != strings.ToLower(row.Email) {
		return nil, fmt.Errorf("%w: userName is the person's address and this tenant's login lookup; no door here moves it", crud.ErrInvalid)
	}
	changed := make([]string, 0, 3)
	name := contracts.FoldName(in.DisplayName, in.Name)
	wrote, err := s.rename(ctx, tx, id, name)
	if err != nil {
		return nil, err
	}
	if wrote {
		changed = append(changed, "displayName")
	}
	if in.Active != nil {
		if !*in.Active {
			if err := s.close(ctx, tx, row); err != nil {
				return nil, err
			}
		} else if row.Status != usercontracts.StatusActive {
			// The one activation door reachable from here, and the same judgement PATCH
			// makes. Both halves of `active` are published by modules/user under their own
			// names — user.email_verified, user.reactivated and user.deactivated — so
			// neither is added to the list below: one fact, one event, whoever opened the
			// door.
			if err := s.open(ctx, tx, row); err != nil {
				return nil, err
			}
		}
	}
	wrote, err = s.setExternal(ctx, tx, id, in.ExternalID)
	if err != nil {
		return nil, err
	}
	if wrote {
		changed = append(changed, "externalId")
	}
	if err := s.recordPersonChanged(ctx, tx, id, changed...); err != nil {
		return nil, err
	}
	return s.GetUser(ctx, tx, id)
}

// PatchUser applies the operations this module reads: active, externalId and the
// display name, named in the path or, as Okta sends them, as members of a
// pathless replace's value (see expandOperations). Anything else refuses the
// whole PATCH — the protocol says an operation that cannot be applied fails the
// request, and a partial application would leave the connector's idea of the
// person and this tenant's two different things with no record of the difference.
func (s *Service) PatchUser(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, ops []contracts.Operation) (*contracts.UserResource, error) {
	if err := s.mayProvision(ctx, tx); err != nil {
		return nil, err
	}
	row, err := readUser(tx, id)
	if err != nil {
		return nil, err
	}
	name := row.DisplayName
	external := row.ExternalID
	active := new(bool)
	*active = row.Status == usercontracts.StatusActive
	wroteName, wroteExternal, wroteActive := false, false, false
	expanded, err := expandOperations(ops)
	if err != nil {
		return nil, err
	}
	for _, op := range expanded {
		path := strings.ToLower(strings.TrimSpace(op.Path))
		switch path {
		case "active":
			v, err := patchBool(op)
			if err != nil {
				return nil, err
			}
			*active, wroteActive = v, true
		case "externalid":
			v, err := patchString(op)
			if err != nil {
				return nil, err
			}
			external, wroteExternal = v, true
		case "displayname", "name.formatted", "name.givenname", "name.familyname":
			v, err := patchString(op)
			if err != nil {
				return nil, err
			}
			name, wroteName = v, true
		case "username":
			return nil, fmt.Errorf("%w: userName is the person's address and this tenant's login lookup; no door here moves it", crud.ErrInvalid)
		default:
			return nil, fmt.Errorf("%w: %q is not an attribute this service writes", crud.ErrInvalid, op.Path)
		}
	}
	changed := make([]string, 0, 2)
	if wroteName {
		wrote, err := s.rename(ctx, tx, id, name)
		if err != nil {
			return nil, err
		}
		if wrote {
			changed = append(changed, "displayName")
		}
	}
	if wroteActive {
		if !*active {
			if err := s.close(ctx, tx, row); err != nil {
				return nil, err
			}
		} else if row.Status != usercontracts.StatusActive {
			if err := s.open(ctx, tx, row); err != nil {
				return nil, err
			}
		}
	}
	if wroteExternal {
		wrote, err := s.setExternal(ctx, tx, id, external)
		if err != nil {
			return nil, err
		}
		if wrote {
			changed = append(changed, "externalId")
		}
	}
	// One line for one request, naming what actually moved. A PATCH that repeated
	// what the person was already called changed nobody and writes nothing, and an
	// event saying otherwise would be a false entry in the one record a person
	// reads to find out what a connector did to their directory.
	if err := s.recordPersonChanged(ctx, tx, id, changed...); err != nil {
		return nil, err
	}
	return s.GetUser(ctx, tx, id)
}

// DeactivateUser is what DELETE answers here, and the word is the whole design
// note: the person stops being able to sign in, the row keeps its address
// reserved, and the history stays. A connector that reprovisions them later finds
// the same person back rather than a second one.
func (s *Service) DeactivateUser(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) error {
	if err := s.mayProvision(ctx, tx); err != nil {
		return err
	}
	row, err := readUser(tx, id)
	if err != nil {
		return err
	}
	return s.close(ctx, tx, row)
}

// userNameOf is what a replace claims the address is. A draft that names none is
// the caller saying nothing, which is not a change and is not a refusal.
func userNameOf(in contracts.UserDraft, row userRow) string {
	if v := strings.TrimSpace(in.UserName); v != "" {
		return v
	}
	return row.Email
}
