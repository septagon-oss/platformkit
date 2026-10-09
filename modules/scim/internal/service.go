package internal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/scim/contracts"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// Service is the SCIM surface over this tenant's people and groups.
//
// It holds two collaborators and no state of its own beyond them, which is what
// lets one value serve a request, a job and an event handler at once. Neither is
// a thing this module may do for itself: the person is modules/user's row, and
// what a role name grants is the auth module's table. Both arrive at composition.
type Service struct {
	users usercontracts.Service
	roles contracts.RoleDirectory
}

// NewService returns the service. module.go builds it and refuses a composition
// that named neither collaborator.
func NewService(users usercontracts.Service, roles contracts.RoleDirectory) *Service {
	return &Service{users: users, roles: roles}
}

var _ contracts.Service = (*Service)(nil)

// Group is this module's own half of a collection the identity provider names.
//
// The members are a table, not a column, because two connectors writing one
// collection is the normal case rather than an edge, and the role is another
// table because a group whose name nobody has mapped is a group a provider can
// mirror today (see the migration).
type Group struct {
	entity.Base
	Name       string `gorm:"column:name"`
	ExternalID string `gorm:"column:external_id"`
}

// TableName is the table the migration creates.
func (Group) TableName() string { return "scim_groups" }

// GroupMapping binds one group to the role its membership grants. An empty role
// is the row's way of saying the mapping was withdrawn, which is a row rather
// than a deletion so that "who removed this, and when" has somewhere to be.
type GroupMapping struct {
	entity.Base
	GroupID uuid.UUID `gorm:"column:group_id"`
	Role    string    `gorm:"column:role"`
}

// TableName is the table the migration creates.
func (GroupMapping) TableName() string { return "scim_group_mappings" }

// userRow is one person as the four tables this module reads describe them. It
// is a query's shape and not an entity: the person's own columns stay
// modules/user's, and this module reads them rather than mirroring them.
type userRow struct {
	ID          uuid.UUID  `gorm:"column:id"`
	Email       string     `gorm:"column:email"`
	DisplayName string     `gorm:"column:display_name"`
	Status      string     `gorm:"column:status"`
	CreatedAt   time.Time  `gorm:"column:created_at"`
	UpdatedAt   time.Time  `gorm:"column:updated_at"`
	DeletedAt   *time.Time `gorm:"column:deleted_at"`
	ExternalID  string     `gorm:"column:external_id"`
}

func (r userRow) resource(groups []contracts.GroupRef) *contracts.UserResource {
	active := r.Status == usercontracts.StatusActive
	out := &contracts.UserResource{
		Schemas:    []string{contracts.SchemaUser},
		ID:         r.ID,
		ExternalID: r.ExternalID,
		UserName:   r.Email,
		Emails:     []contracts.Email{{Value: r.Email, Type: "work", Primary: true}},
		Active:     active,
		Groups:     groups,
		Meta: contracts.Meta{
			ResourceType: contracts.ResourceTypeUser,
			Created:      utc(r.CreatedAt), LastModified: utc(r.UpdatedAt),
			Version: utc(r.UpdatedAt),
		},
	}
	if r.DisplayName != "" {
		out.DisplayName = r.DisplayName
		out.Name = &contracts.Name{Formatted: r.DisplayName}
	}
	return out
}

// memberRow is one membership as the group door reads it.
type memberRow struct {
	ID          uuid.UUID `gorm:"column:id"`
	Email       string    `gorm:"column:email"`
	DisplayName string    `gorm:"column:display_name"`
}

// groupRow is one group with its mapping.
type groupRow struct {
	ID        uuid.UUID  `gorm:"column:id"`
	Name      string     `gorm:"column:name"`
	ExtID     string     `gorm:"column:external_id"`
	Role      string     `gorm:"column:role"`
	CreatedAt time.Time  `gorm:"column:created_at"`
	UpdatedAt time.Time  `gorm:"column:updated_at"`
	DeletedAt *time.Time `gorm:"-"`
}

// resource assembles the document. Created and lastModified come from the row: a
// group this module wrote carries both, and a list that reads many rows reads
// them with the same columns, so no page answers a different shape than a
// single read does.
func (r groupRow) resource(members []contracts.MemberRef) *contracts.GroupResource {
	at := r.CreatedAt
	return &contracts.GroupResource{
		Schemas:     []string{contracts.SchemaGroup},
		ID:          r.ID,
		ExternalID:  r.ExtID,
		DisplayName: r.Name,
		Members:     members,
		Role:        r.Role,
		Meta: contracts.Meta{
			ResourceType: contracts.ResourceTypeGroup,
			Created:      utc(at), LastModified: utc(at), Version: utc(at),
		},
	}
}

// utc is how a timestamp leaves this module. Every one of them is UTC already —
// the database stores timestamptz and the kernel reads it as such — and the
// protocol names RFC 3339, so the format is written here once rather than at
// each of the dozen places a meta is assembled.
func utc(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

// pageBounds turns the protocol's startIndex/count into the two bounds the
// kernel's own lists use. SCIM counts from 1; the database counts from 0.
func pageBounds(q contracts.ListQuery) (limit, offset int) {
	limit = q.Count
	if limit <= 0 {
		limit = crud.DefaultLimit
	}
	if limit > crud.MaxLimit {
		limit = crud.MaxLimit
	}
	offset = q.StartIndex - 1
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

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
		groups, err := s.groupsOf(tx, row.ID)
		if err != nil {
			return nil, err
		}
		out.Resources = append(out.Resources, row.resource(groups))
	}
	return out, nil
}

// groupsOf is the read-only half of membership: which of the provider's
// collections this person is in. It is a query and not a column because the
// membership lives in one table with one door — the Groups one.
func (s *Service) groupsOf(tx db.Tx[db.Tenant], userID uuid.UUID) ([]contracts.GroupRef, error) {
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
	groups, err := s.groupsOf(tx, id)
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
			if _, err := s.users.Deactivate(ctx, tx, id); err != nil {
				return nil, err
			}
		} else if row.Status != usercontracts.StatusActive {
			// The one activation door reachable from here, and the same refusal PATCH
			// makes: a closed account or one awaiting a person's own approval is not
			// reopened by an integration, because the approval is a person's act. Both
			// halves of `active` are published by modules/user under their own names —
			// user.email_verified and user.deactivated — so neither is added to the
			// list below: one fact, one event, whoever opened the door.
			if _, err := s.users.ConfirmAddress(ctx, tx, id, row.Email); err != nil {
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
			if _, err := s.users.Deactivate(ctx, tx, id); err != nil {
				return nil, err
			}
		} else if row.Status != usercontracts.StatusActive {
			// An identity provider that says this person is active is saying it has
			// the mailbox. That is ConfirmAddress's fact, and it is refused for a
			// person whose account is closed or awaiting a person's own approval —
			// an approval is a person's act, and no integration performs it.
			if _, err := s.users.ConfirmAddress(ctx, tx, id, row.Email); err != nil {
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
	if _, err := readUser(tx, id); err != nil {
		return err
	}
	_, err := s.users.Deactivate(ctx, tx, id)
	return err
}

// rename writes the one name this platform keeps, and only when it changed. The
// user module owns the row, so this goes through the generic write its own Spec
// would take rather than an UPDATE of a table this module does not own.
//
// It answers whether the row moved, because the caller has to say so in the trail:
// modules/user publishes an event for every command it owns and owns no command
// for a name, so a rename that arrived through this door would otherwise be the
// one person-side write in the platform with no record that it happened.
func (s *Service) rename(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, name string) (bool, error) {
	u, err := s.users.Get(ctx, tx, id)
	if err != nil {
		return false, err
	}
	if u.DisplayName == name {
		return false, nil
	}
	u.DisplayName = name
	return true, crud.Update(ctx, tx, u, "display_name", "updated_at")
}

// recordPersonChanged writes the trail's record of an attribute of a person that
// no event under a `user.` name describes. The list arrives already narrowed to
// what changed, and an empty one writes nothing: a request that restated what was
// already there is not an event, and the outbox is where an auditor reads what a
// connector did.
func (s *Service) recordPersonChanged(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, changed ...string) error {
	if len(changed) == 0 {
		return nil
	}
	return events.Publish(ctx, tx, contracts.EventUserChanged, contracts.UserChanged{
		User: id, Changed: changed, At: db.Now(),
	})
}

// setExternal records the identifier the provider calls this person by. An empty
// one withdraws it, which is a write rather than a deletion so that a connector
// that stops sending the id is not indistinguishable from one that never had it.
//
// It answers whether the row moved for the same reason rename does, and the
// reason is stronger here: this table is this module's own, so no other module's
// event could ever describe what happened to it.
func (s *Service) setExternal(ctx context.Context, tx db.Tx[db.Tenant], userID uuid.UUID, external string) (bool, error) {
	external = strings.TrimSpace(external)
	var held struct {
		UserID     uuid.UUID `gorm:"column:user_id"`
		ExternalID string    `gorm:"column:external_id"`
	}
	err := tx.DB().Table("scim_user_external").
		Where("user_id = ? AND tenant_id = ?", userID, db.TenantOf(tx).ID).Take(&held).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		if external == "" {
			return false, nil
		}
		if err := tx.DB().Table("scim_user_external").Create(map[string]any{
			"tenant_id": db.TenantOf(tx).ID, "user_id": userID, "external_id": external,
		}).Error; err != nil {
			return false, err
		}
		return true, nil
	case err != nil:
		return false, fmt.Errorf("scim: read external id: %w", err)
	}
	if held.ExternalID == external {
		// The same identifier again is not a write, and it is not an event either:
		// a connector that re-sent what it had already sent changed nobody's record,
		// and the trail would otherwise fill with a retry's noise.
		return false, nil
	}
	if err := tx.DB().Table("scim_user_external").
		Where("user_id = ? AND tenant_id = ?", userID, db.TenantOf(tx).ID).
		Update("external_id", external).Error; err != nil {
		return false, err
	}
	return true, nil
}

// ListGroups pages the tenant's collections. The filter is refused the same way
// a user filter is: displayName eq would be a filter on a name this module
// refuses to rename, and answering it unfiltered would let a connector act on
// every group it meant to skip.
func (s *Service) ListGroups(_ context.Context, tx db.Tx[db.Tenant], q contracts.ListQuery) (*contracts.GroupList, error) {
	if strings.TrimSpace(q.Filter) != "" {
		return nil, fmt.Errorf("%w: %q; this service lists groups unfiltered", crud.ErrInvalid, q.Filter)
	}
	limit, offset := pageBounds(q)
	var total int64
	if err := tx.DB().Table("scim_groups").Where("tenant_id = ?", db.TenantOf(tx).ID).Count(&total).Error; err != nil {
		return nil, fmt.Errorf("scim: count groups: %w", err)
	}
	rows, err := readGroups(tx, "g.id, g.name, g.external_id, COALESCE(m.role, '') AS role, g.created_at, g.updated_at", limit, offset, "")
	if err != nil {
		return nil, err
	}
	out := &contracts.GroupList{
		Schemas: []string{contracts.SchemaListResponse}, TotalResults: int(total),
		StartIndex: offset + 1, ItemsPerPage: limit,
	}
	for _, r := range rows {
		members, err := s.members(tx, r.ID)
		if err != nil {
			return nil, err
		}
		out.Resources = append(out.Resources, r.resource(members))
	}
	return out, nil
}

// readGroups is the one query that reads a group with its mapping, asked three
// ways — the list, one group, and one group by name.
func readGroups(tx db.Tx[db.Tenant], columns string, limit, offset int, name string) ([]groupRow, error) {
	db_ := tx.DB().Table("scim_groups g").
		Where("g.tenant_id = ?", db.TenantOf(tx).ID)
	if name != "" {
		db_ = db_.Where("lower(g.name) = lower(?)", name)
	}
	var rows []groupRow
	err := db_.Select(columns).
		Joins("LEFT JOIN scim_group_mappings m ON m.group_id = g.id").
		Order("lower(g.name), g.id").Limit(limit).Offset(offset).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("scim: read groups: %w", err)
	}
	return rows, nil
}

func (s *Service) members(tx db.Tx[db.Tenant], groupID uuid.UUID) ([]contracts.MemberRef, error) {
	var rows []memberRow
	err := tx.DB().Table("users u").Select("u.id, u.email, u.display_name").
		Joins("JOIN scim_group_members m ON m.user_id = u.id").
		Where("m.group_id = ? AND u.tenant_id = ? AND u.deleted_at IS NULL", groupID, db.TenantOf(tx).ID).
		Order("lower(u.email), u.id").Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("scim: read members: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	out := make([]contracts.MemberRef, 0, len(rows))
	for _, r := range rows {
		display := r.DisplayName
		if display == "" {
			display = r.Email
		}
		out = append(out, contracts.MemberRef{Value: r.ID.String(), Display: display, Type: "DIRECT"})
	}
	return out, nil
}

// GetGroup is one group of this tenant, with its members.
func (s *Service) GetGroup(_ context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*contracts.GroupResource, error) {
	row, err := readGroup(tx, id)
	if err != nil {
		return nil, err
	}
	members, err := s.members(tx, id)
	if err != nil {
		return nil, err
	}
	return row.resource(members), nil
}

// readGroup is one group by id, or ErrNotFound, which is what a 404 needs.
func readGroup(tx db.Tx[db.Tenant], id uuid.UUID) (groupRow, error) {
	var row groupRow
	err := tx.DB().Table("scim_groups g").
		Select("g.id, g.name, g.external_id, COALESCE(m.role, '') AS role, g.created_at, g.updated_at").
		Joins("LEFT JOIN scim_group_mappings m ON m.group_id = g.id").
		Where("g.id = ? AND g.tenant_id = ?", id, db.TenantOf(tx).ID).Take(&row).Error
	if err != nil {
		return groupRow{}, crud.Classify(err)
	}
	return row, nil
}

// readGroupForUpdate is the read every write of a group starts with: the row's
// own lock first, then the row.
//
// Checking that a collection exists and then writing what hangs off it is two
// statements, and between them a DELETE can commit — a connector that deleted the
// group while another was adding a member would leave a membership pointing at an
// id no group answers, which no read of either table can explain afterwards. The
// lock makes the second writer wait for the first to finish and then answer that
// there is nothing there to write to, which is also what its caller's transaction
// rolls back.
func readGroupForUpdate(tx db.Tx[db.Tenant], id uuid.UUID) (groupRow, error) {
	if err := lockGroup(tx, id); err != nil {
		return groupRow{}, err
	}
	return readGroup(tx, id)
}

// lockGroup holds one group's row for the length of this transaction.
func lockGroup(tx db.Tx[db.Tenant], id uuid.UUID) error {
	var held struct {
		ID uuid.UUID `gorm:"column:id"`
	}
	err := tx.DB().Table("scim_groups").Select("id").
		Where("id = ? AND tenant_id = ?", id, db.TenantOf(tx).ID).
		Clauses(clause.Locking{Strength: "UPDATE"}).Take(&held).Error
	if err != nil {
		return crud.Classify(err)
	}
	return nil
}

// CreateGroup declares a collection. Its name is the connector's, compared
// without case, and it is refused when this tenant already has that collection
// under another spelling: two groups called "Finance" and "finance" are two rows
// that both map to one role, and the mapping is one row.
//
// It declares a collection and nothing else: the role it brings is refused here
// (see refusedGroup) and is written by MapGroup, so a provisioning credential can
// mirror a directory but cannot decide what mirroring it grants.
func (s *Service) CreateGroup(ctx context.Context, tx db.Tx[db.Tenant], in contracts.GroupDraft) (*contracts.GroupResource, error) {
	if err := refusedGroup(in); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(in.DisplayName)
	if name == "" {
		return nil, fmt.Errorf("%w: a group is named by displayName", crud.ErrInvalid)
	}
	if _, err := s.groupByName(ctx, tx, name); err == nil {
		return nil, fmt.Errorf("%w: this tenant already has a group called %q", crud.ErrConflict, name)
	} else if !errors.Is(err, crud.ErrNotFound) {
		return nil, err
	}
	g := Group{Name: name, ExternalID: strings.TrimSpace(in.ExternalID)}
	if err := crud.Create(ctx, tx, &g); err != nil {
		return nil, err
	}
	if err := s.replaceMembers(ctx, tx, g.ID, in.Members); err != nil {
		return nil, err
	}
	if err := s.recordGroupCreated(ctx, tx, g); err != nil {
		return nil, err
	}
	return s.GetGroup(ctx, tx, g.ID)
}

// ReplaceGroup is a PUT. Its name must already be this group's: a rename would
// move the role mapping that a person set up for the old name, and the protocol
// has no verb for "the same collection, called something else". Delete and create
// are the explicit act, and they are both refused while the group has members, so
// a rename has to be decided rather than performed by accident.
func (s *Service) ReplaceGroup(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, in contracts.GroupDraft) (*contracts.GroupResource, error) {
	if err := refusedGroup(in); err != nil {
		return nil, err
	}
	g, err := readGroupForUpdate(tx, id)
	if err != nil {
		return nil, err
	}
	if want := strings.TrimSpace(in.DisplayName); want != "" && !strings.EqualFold(want, g.Name) {
		return nil, fmt.Errorf("%w: a group's name is the key its role mapping was made for; delete it and declare the new one", crud.ErrInvalid)
	}
	if in.Members != nil {
		if err := s.replaceMembers(ctx, tx, id, in.Members); err != nil {
			return nil, err
		}
	}
	if external := strings.TrimSpace(in.ExternalID); external != "" && external != g.ExtID {
		if err := tx.DB().Model(&Group{}).Where("id = ?", id).Update("external_id", external).Error; err != nil {
			return nil, fmt.Errorf("scim: replace group: %w", err)
		}
	}
	if err := s.recordGroupChanged(ctx, tx, id, g.Name, "replace"); err != nil {
		return nil, err
	}
	return s.GetGroup(ctx, tx, id)
}

// PatchGroup reads the members of a collection: add, remove and replace, whole or
// one person named in a valuePath. The role extension is not one of the paths —
// see refusedGroup for why the door refuses it whoever sends it.
func (s *Service) PatchGroup(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, ops []contracts.Operation) (*contracts.GroupResource, error) {
	g, err := readGroupForUpdate(tx, id)
	if err != nil {
		return nil, err
	}
	expanded, err := expandOperations(ops)
	if err != nil {
		return nil, err
	}
	for _, op := range expanded {
		path := strings.ToLower(strings.TrimSpace(op.Path))
		if path == "members" || strings.HasPrefix(path, "members[") {
			who, err := memberSelection(path)
			if err != nil {
				return nil, err
			}
			members, err := patchMembers(op)
			if err != nil {
				return nil, err
			}
			if len(members) == 0 && who != "" {
				members = []contracts.MemberRef{{Value: who}}
			}
			if err := s.moveMembers(ctx, tx, id, strings.ToLower(strings.TrimSpace(op.Op)), members); err != nil {
				return nil, err
			}
			continue
		}
		return nil, fmt.Errorf("%w: %q is not an attribute this service writes", crud.ErrInvalid, op.Path)
	}
	if err := s.recordGroupChanged(ctx, tx, id, g.Name, "patch"); err != nil {
		return nil, err
	}
	return s.GetGroup(ctx, tx, id)
}

// DeleteGroup removes the collection and its mapping. It refuses a group with
// members: membership has one door, and a connector that deleted a group out from
// under people would silently take their role with it.
func (s *Service) DeleteGroup(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) error {
	g, err := readGroupForUpdate(tx, id)
	if err != nil {
		return err
	}
	var n int64
	if err := tx.DB().Table("scim_group_members").Where("group_id = ?", id).Count(&n).Error; err != nil {
		return fmt.Errorf("scim: count members: %w", err)
	}
	if n > 0 {
		return fmt.Errorf("%w: %d member(s); remove them through the same door that added them", crud.ErrConflict, n)
	}
	if err := tx.DB().Where("group_id = ?", id).Delete(&GroupMapping{}).Error; err != nil {
		return fmt.Errorf("scim: remove mapping: %w", err)
	}
	if err := crud.Delete[*Group](tx, id, false); err != nil {
		return err
	}
	return events.Publish(ctx, tx, contracts.EventGroupRemoved, contracts.GroupRemoved{
		Group: id, DisplayName: g.Name, Role: g.Role, At: db.Now(),
	})
}

// GroupMappings is the tenant's mapping table, read for the workspace door. The
// group's name comes back rather than its id, because the screen that shows this
// is a list of what the provider calls things.
func (s *Service) GroupMappings(_ context.Context, tx db.Tx[db.Tenant]) (contracts.MappingList, error) {
	var rows []struct {
		Name string `gorm:"column:name"`
		Role string `gorm:"column:role"`
	}
	err := tx.DB().Table("scim_group_mappings m").Select("g.name, m.role").
		Joins("JOIN scim_groups g ON g.id = m.group_id").
		Where("g.tenant_id = ? AND m.role <> ''", db.TenantOf(tx).ID).
		Order("lower(g.name)").Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("scim: read mappings: %w", err)
	}
	out := make(contracts.MappingList, 0, len(rows))
	for _, r := range rows {
		out = append(out, &contracts.GroupMapping{Group: r.Name, Role: r.Role})
	}
	return out, nil
}

// MapGroup binds a group name to a role, or withdraws the binding when role is
// empty. It is the workspace's write, guarded by user:manage, and it refuses a
// role this tenant has never been told about: the alternative is a mapping that
// grants nothing, discovered the day somebody expects it to have granted
// something.
//
// It is also the only writer of the mapping table anywhere in this module. That
// is the point of the door rather than a comment on it: the protocol has no field
// to hold a role, and a credential a tenant hands to somebody else's identity
// provider must not be able to say what a collection it mirrors grants.
func (s *Service) MapGroup(ctx context.Context, tx db.Tx[db.Tenant], group, role string) (*contracts.GroupMapping, error) {
	if err := mayBindMapping(ctx); err != nil {
		return nil, err
	}
	g, err := s.groupByName(ctx, tx, group)
	if err != nil {
		return nil, err
	}
	want := strings.TrimSpace(role)
	// The same lock the membership writes take: without it a group deleted in
	// another transaction would leave this one's binding pointing at a row that is
	// on its way out, and a mapping of that kind is read by nobody and cleaned up
	// by nothing.
	if err := lockGroup(tx, g.ID); err != nil {
		return nil, err
	}
	was, err := s.mapGroup(ctx, tx, g.ID, want)
	if err != nil {
		return nil, err
	}
	if was == want {
		// The same answer twice changed nobody's authority, and an event saying it
		// did would be a false line in the one record a person reads to find out
		// who decided what a membership means.
		return &contracts.GroupMapping{Group: g.Name, Role: want}, nil
	}
	if err := events.Publish(ctx, tx, contracts.EventMappingChanged, contracts.MappingChanged{
		Group: g.ID, DisplayName: g.Name, Was: was, Now: want, At: db.Now(),
	}); err != nil {
		return nil, err
	}
	return &contracts.GroupMapping{Group: g.Name, Role: want}, nil
}

// mayBindMapping is MapGroup's own recheck of who is asking, made inside the
// transaction the binding is written in.
//
// The guard on the door is not the whole rule. MapGroup is exported, and this
// module is called by other modules: a collaborator that took the request's own
// transaction and called this by name would otherwise write a grant its actor
// never held, and the answer would look like any other mapping — because it *is*
// any other mapping, read back by every member of that group from that day. The
// whole point of the tenant's provisioning credential is that it mirrors a
// directory it does not own and cannot decide what mirroring it costs, and a rule
// that holds only for whoever happens to enter through the HTTP door is that rule
// with a hole in the shape of every other caller.
//
// What a module can decide for itself is the caller's own ceiling.
// tenancy.Principal.Permissions is the authority a credential carries, and a
// non-nil list is a ceiling and never an addition — so a caller whose credential
// carries one is held to it here, by exact name, whoever it is and however it
// reached this function. A refusal reads nothing, writes nothing, publishes
// nothing and returns no row, and it arrives as the kernel's own 403 for a caller
// that reached it through a request.
//
// What stays with the door is the question the module cannot answer: which
// permissions a signed-in caller's roles carry is the auth module's table, and
// reading it would be two modules' rows in one query (the same split as
// RoleDirectory, answered at composition rather than reached across). So a caller
// that carries no scope of its own is refused only by the guard its route
// declared, and that limit is stated in the module's README and asked about by
// TestMappingCommandRefusesAProvisionOnlyActor instead of being argued away.
func mayBindMapping(ctx context.Context) error {
	p, ok := tenancy.PrincipalFrom(ctx)
	if !ok || p.Permissions == nil {
		return nil
	}
	if slices.Contains(p.Permissions, usercontracts.PermissionUserManage) {
		return nil
	}
	return fmt.Errorf("%w: binding a group to a role is %s work, and this credential carries its own scope without it",
		tenancy.ErrPolicyDenied, usercontracts.PermissionUserManage)
}

// mapGroup is the one write of the mapping table, from whichever door asked. It
// answers the role the row held beforehand, which is what the trail records and
// what the affected people's roles are measured against.
func (s *Service) mapGroup(ctx context.Context, tx db.Tx[db.Tenant], groupID uuid.UUID, role string) (string, error) {
	role = strings.TrimSpace(role)
	if role != "" {
		known, err := s.roles.HasRole(ctx, tx, role)
		if err != nil {
			return "", err
		}
		if !known {
			return "", fmt.Errorf("%w: this tenant has no role called %q to bind a group to", crud.ErrInvalid, role)
		}
	}
	// Which roles the mappings own is read *before* the row changes. The role being
	// withdrawn is exactly the one that stops being named by this table the moment
	// the write lands, and a list recomputed afterwards reads it as a grant an
	// administrator made by hand — which is how a withdrawn mapping came to leave
	// the role it withdrew assigned to everybody it used to cover.
	attributable, err := mappedRoles(tx)
	if err != nil {
		return "", err
	}
	var m GroupMapping
	err = tx.DB().Where("group_id = ?", groupID).Take(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if role == "" {
			return "", nil
		}
		m = GroupMapping{GroupID: groupID, Role: role}
		if err := crud.Create(ctx, tx, &m); err != nil {
			return "", err
		}
		first, err := membersOf(tx, groupID)
		if err != nil {
			return "", err
		}
		return "", s.syncRoles(ctx, tx, first, append(attributable, role))
	}
	if err != nil {
		return "", fmt.Errorf("scim: read mapping: %w", err)
	}
	if m.Role == role {
		return m.Role, nil
	}
	was := m.Role
	m.Role = role
	if err := crud.Update(ctx, tx, &m, "role"); err != nil {
		return was, err
	}
	// The binding changed, so every member's roles are recomputed: this is the
	// write that turns an organisational unit mirrored all afternoon into the role
	// the administrator just decided it meant.
	left, err := membersOf(tx, groupID)
	if err != nil {
		return was, err
	}
	return was, s.syncRoles(ctx, tx, left, append(attributable, role))
}

func (s *Service) groupByName(_ context.Context, tx db.Tx[db.Tenant], name string) (groupRow, error) {
	rows, err := readGroups(tx, "g.id, g.name, g.external_id, COALESCE(m.role, '') AS role, g.created_at, g.updated_at", 2, 0, name)
	if err != nil {
		return groupRow{}, err
	}
	if len(rows) == 0 {
		return groupRow{}, fmt.Errorf("%w: no group called %q in this tenant", crud.ErrNotFound, name)
	}
	return rows[0], nil
}

// replaceMembers is a membership list arrived whole. Every member is a person of
// this tenant — an id the tenant cannot see is refused rather than skipped,
// because a connector that quietly lost half its members would report success.
func (s *Service) replaceMembers(ctx context.Context, tx db.Tx[db.Tenant], groupID uuid.UUID, members []contracts.MemberRef) error {
	keep := make([]uuid.UUID, 0, len(members))
	for _, m := range members {
		id, err := uuid.Parse(strings.TrimSpace(m.Value))
		if err != nil {
			return fmt.Errorf("%w: a member names %q, which is not a person's id", crud.ErrInvalid, m.Value)
		}
		if _, err := readUser(tx, id); err != nil {
			return fmt.Errorf("%w: member %s is not a person of this tenant", crud.ErrInvalid, id)
		}
		keep = append(keep, id)
	}
	// Read who was there and which roles the mappings own before the list moves:
	// the rule syncRoles applies is decided against the state the write is made
	// from, not the state it leaves behind.
	attributable, err := mappedRoles(tx)
	if err != nil {
		return err
	}
	left, err := membersOf(tx, groupID)
	if err != nil {
		return err
	}
	if err := tx.DB().Where("group_id = ?", groupID).Delete(&member{}).Error; err != nil {
		return fmt.Errorf("scim: clear members: %w", err)
	}
	if err := s.addMembers(ctx, tx, groupID, keep); err != nil {
		return err
	}
	// The people who left are as much affected as the people who arrived: one
	// brought a role with them and the other took one away.
	return s.syncRoles(ctx, tx, append(append([]uuid.UUID{}, left...), keep...), attributable)
}

// mappedRoles is which roles this tenant's bindings own. That set is the whole
// difference between a role a membership may take away and a role a person was
// given by hand, so every write that recomputes anybody's roles reads it before
// it writes, while the decision is still being made.
func mappedRoles(tx db.Tx[db.Tenant]) ([]string, error) {
	var roles []string
	if err := tx.DB().Table("scim_group_mappings").Distinct().Select("role").
		Where("tenant_id = ? AND role <> ''", db.TenantOf(tx).ID).Scan(&roles).Error; err != nil {
		return nil, fmt.Errorf("scim: read mapped roles: %w", err)
	}
	return roles, nil
}

// membersOf is who is in a group right now, which a write has to know before it
// changes the list rather than after.
func membersOf(tx db.Tx[db.Tenant], groupID uuid.UUID) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	if err := tx.DB().Table("scim_group_members").Select("user_id").
		Where("group_id = ?", groupID).Scan(&ids).Error; err != nil {
		return nil, fmt.Errorf("scim: read members: %w", err)
	}
	return ids, nil
}

// syncRoles is where a group's membership becomes a role.
//
// The rule is one sentence and the whole design is in it: a role that any of
// this tenant's mappings names is the mapping's to grant and to take, and a role
// no mapping names is nobody's here. So a person's roles become the ones their
// collections bring plus whatever an administrator granted them at the workspace
// door, and a connector that removes somebody from Finance takes `finance` and
// nothing else. Without that split there are two owners of one column: the
// mapping would silently ungrant a role a person was given by hand, or a hand
// grant would silently survive the collection that was supposed to mean it.
//
// The order of the two reads below is the whole of the concurrency argument, and
// it is not interchangeable with the order the sentences above fall into. A
// membership write holds its *group's* lock, and two groups have two locks: a
// person in both is being rewritten by two writers at once, each holding the one
// row the other does not want. So each affected person is locked first, in
// ascending id order so two writers cannot cross, and only then are the two facts
// `want` is made of read — which of their collections they are still in, and which
// roles the row already carries. Read before the lock, those two facts are a
// snapshot of a moment that the other writer is spending: both writers compute
// from the same stale roles, the one that finishes last writes back the role the
// first one revoked, and the revocation is gone with nothing left to say it ever
// happened. `SetRoles` locking the row again afterwards buys nothing here, because
// by then the decision it would recheck has already been made from the old state.
//
// SetRoles is the user module's command, so the write publishes user.roles_set,
// rechecks the caller's right to hand the role out, and holds the row while it
// decides. It is refused — and the whole membership write with it, in this same
// transaction — when the grant or the removal would leave the tenant nobody able
// to administer it.
//
// attributable is the caller's reading of the mapping table, taken before the
// write that is in hand: every caller reads it while its own decision is still
// open rather than from the table it is about to change.
func (s *Service) syncRoles(ctx context.Context, tx db.Tx[db.Tenant], ids []uuid.UUID, attributable []string) error {
	unique := make([]uuid.UUID, 0, len(ids))
	seen := map[uuid.UUID]bool{}
	for _, id := range ids {
		if !seen[id] {
			seen[id], unique = true, append(unique, id)
		}
	}
	if len(unique) == 0 {
		return nil
	}
	// The lock order, fixed here rather than left to whichever group arrived
	// first: a writer that locked people in the order its own group listed them
	// could be holding the one the other writer is waiting for.
	slices.SortFunc(unique, func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) })
	granted := make(map[string]bool, len(attributable))
	for _, r := range attributable {
		granted[r] = true
	}
	for _, id := range unique {
		u, err := crud.GetForUpdate[*usercontracts.User](tx, id)
		if err != nil {
			return err
		}
		var brought []string
		err = tx.DB().Table("scim_group_members gm").Distinct().Select("m.role").
			Joins("JOIN scim_group_mappings m ON m.group_id = gm.group_id").
			Where("gm.user_id = ? AND m.role <> ''", id).Scan(&brought).Error
		if err != nil {
			return fmt.Errorf("scim: read the roles this person's groups bring: %w", err)
		}
		want := make([]string, 0, len(u.Roles)+len(brought))
		for _, r := range u.Roles {
			if !granted[r] {
				want = append(want, r)
			}
		}
		have := make(map[string]bool, len(want))
		for _, r := range want {
			have[r] = true
		}
		for _, r := range brought {
			if !have[r] {
				have[r], want = true, append(want, r)
			}
		}
		slices.Sort(want)
		if same(u.Roles, want) {
			continue
		}
		if _, err := s.users.SetRoles(ctx, tx, id, want); err != nil {
			return err
		}
	}
	return nil
}

// same compares two role sets, in whatever order they arrived.
func same(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	x := append([]string{}, a...)
	y := append([]string{}, b...)
	slices.Sort(x)
	slices.Sort(y)
	return slices.Equal(x, y)
}

// member is scim_group_members as a write target. It carries no Base: the table
// has no id, and an entity with a primary key it does not have is a row crud
// would try to update by a column that is not there.
type member struct {
	TenantID uuid.UUID `gorm:"column:tenant_id"`
	GroupID  uuid.UUID `gorm:"column:group_id"`
	UserID   uuid.UUID `gorm:"column:user_id"`
}

func (member) TableName() string { return "scim_group_members" }

// moveMembers applies one add or remove operation.
func (s *Service) moveMembers(ctx context.Context, tx db.Tx[db.Tenant], groupID uuid.UUID, op string, members []contracts.MemberRef) error {
	ids := make([]uuid.UUID, 0, len(members))
	for _, m := range members {
		id, err := uuid.Parse(strings.TrimSpace(m.Value))
		if err != nil {
			return fmt.Errorf("%w: a member names %q, which is not a person's id", crud.ErrInvalid, m.Value)
		}
		ids = append(ids, id)
	}
	left, err := membersOf(tx, groupID)
	if err != nil {
		return err
	}
	attributable, err := mappedRoles(tx)
	if err != nil {
		return err
	}
	switch op {
	case "add":
		if err := s.addMembers(ctx, tx, groupID, ids); err != nil {
			return err
		}
	case "remove":
		if len(ids) == 0 {
			if err := tx.DB().Where("group_id = ?", groupID).Delete(&member{}).Error; err != nil {
				return fmt.Errorf("scim: clear members: %w", err)
			}
		} else if err := tx.DB().Where("group_id = ? AND user_id IN ?", groupID, ids).
			Delete(&member{}).Error; err != nil {
			return fmt.Errorf("scim: remove members: %w", err)
		}
	case "replace":
		if err := tx.DB().Where("group_id = ?", groupID).Delete(&member{}).Error; err != nil {
			return fmt.Errorf("scim: clear members: %w", err)
		}
		if err := s.addMembers(ctx, tx, groupID, ids); err != nil {
			return err
		}
	default:
		return fmt.Errorf("%w: %q is not an operation on members", crud.ErrInvalid, op)
	}
	// add affects the people named, remove the people named and everybody who was
	// in a group emptied with no names, and replace both. Reading who was there
	// before the write, rather than reasoning about which verb it was, is what
	// keeps this right when a verb gains a third shape.
	after, err := membersOf(tx, groupID)
	if err != nil {
		return err
	}
	return s.syncRoles(ctx, tx, append(append(append([]uuid.UUID{}, left...), ids...), after...), attributable)
}

// addMembers writes membership, refusing a person this tenant cannot see. The
// insert is on conflict do nothing because the same person in the same group
// twice is the protocol's normal noise — two connectors, or one connector that
// retried — and an error would be a write that reports a failure it did not have.
func (s *Service) addMembers(_ context.Context, tx db.Tx[db.Tenant], groupID uuid.UUID, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	tenant := db.TenantOf(tx).ID
	var here []uuid.UUID
	if err := tx.DB().Table("users").Select("id").
		Where("id IN ? AND tenant_id = ? AND deleted_at IS NULL", ids, tenant).
		Scan(&here).Error; err != nil {
		return fmt.Errorf("scim: check members: %w", err)
	}
	// The set is built here rather than asked for with a NOT IN, so the refusal
	// can name the person that is not in this tenant and nobody else's.
	present := make(map[uuid.UUID]bool, len(here))
	for _, id := range here {
		present[id] = true
	}
	for _, id := range ids {
		if !present[id] {
			return fmt.Errorf("%w: member %s is not a person of this tenant", crud.ErrInvalid, id)
		}
	}
	rows := make([]member, 0, len(ids))
	for _, id := range ids {
		rows = append(rows, member{TenantID: tenant, GroupID: groupID, UserID: id})
	}
	return tx.DB().Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error
}

// refused names the attributes no write through this module accepts.
//
// A password is the one that matters. The user module stores a hash no door
// outside it reads (PasswordHash is json:"-"), and its own commands refuse an
// activation that bypasses a person's own choice; an integration that could hand
// over a password would be a door into an account whose person never chose one.
// A schema urn this service does not serve is refused for the same kind of
// reason: it says the client is writing something else.
func refused(in contracts.UserDraft) error {
	if strings.TrimSpace(in.Password) != "" {
		return fmt.Errorf("%w: a password is chosen by the person it belongs to; invite them and let them set one", crud.ErrInvalid)
	}
	for _, s := range in.Schemas {
		if strings.TrimSpace(s) != contracts.SchemaUser {
			return fmt.Errorf("%w: %q is not a schema this service serves", crud.ErrInvalid, s)
		}
	}
	return nil
}

// refusedGroup names what no group write through this module accepts, and the one
// attribute is the role a group brings.
//
// The core Group schema has no place to hold a role: it is an extension this
// platform added, which means the field is not the connector's to send and the
// fact is not its to state. The reason to refuse it rather than read it is the
// reason a tenant hands a scoped credential to a system it does not own: a
// provisioning key that could bind a mirrored organisational unit to a role could
// grant itself any role in the tenant, and it would do so through a door whose
// whole purpose is to write down what some other directory says. So the binding
// is made by a person, at this tenant's own address, under user:manage — see
// MapGroup, the only writer of the table — and a draft that names a role is
// refused where it arrives rather than quietly dropped, which is the same rule
// that stands under a password.

func refusedGroup(in contracts.GroupDraft) error {
	if role := strings.TrimSpace(in.Role); role != "" {
		return fmt.Errorf("%w: %q is a role this tenant binds at its own door (the group-mappings write, under user:manage), not by a provisioning credential; the same POST without the role declares the collection", crud.ErrInvalid, role)
	}
	for _, s := range in.Schemas {
		if strings.TrimSpace(s) != contracts.SchemaGroup {
			return fmt.Errorf("%w: %q is not a schema this service serves", crud.ErrInvalid, s)
		}
	}
	return nil
}

// recordGroupCreated and recordGroupChanged write the trail's record of a group
// this module just changed, in the transaction that changed it: which collection,
// how it changed and how many it now holds. The count is read back rather than
// counted off the request, because the number an auditor wants is what the group
// holds after the write, and the two differ the moment a connector sends an id it
// already had.
func (s *Service) recordGroupCreated(ctx context.Context, tx db.Tx[db.Tenant], g Group) error {
	here, err := membersOf(tx, g.ID)
	if err != nil {
		return err
	}
	return events.Publish(ctx, tx, contracts.EventGroupCreated, contracts.GroupCreated{
		Group: g.ID, DisplayName: g.Name, ExternalID: g.ExternalID, Members: len(here), At: db.Now(),
	})
}

func (s *Service) recordGroupChanged(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, name, via string) error {
	here, err := membersOf(tx, id)
	if err != nil {
		return err
	}
	return events.Publish(ctx, tx, contracts.EventGroupChanged, contracts.GroupChanged{
		Group: id, DisplayName: name, Via: via, Members: len(here), At: db.Now(),
	})
}

// addressOf is the person's address, from whichever field the client sent it in.
// userName is the field; emails[] is read only when userName is absent and the
// list holds exactly one address, because choosing between two of them is a
// decision about which mailbox this person is, and a protocol message that will
// not say is not the place to make it.
func addressOf(in contracts.UserDraft) (string, error) {
	if v := strings.TrimSpace(in.UserName); v != "" {
		return v, nil
	}
	var values []string
	for _, e := range in.Emails {
		if v := strings.TrimSpace(e.Value); v != "" {
			values = append(values, v)
		}
	}
	if len(values) == 1 {
		return values[0], nil
	}
	if len(values) == 0 {
		return "", fmt.Errorf("%w: a person is named by userName or by one email address", crud.ErrInvalid)
	}
	return "", fmt.Errorf("%w: this platform has one address per person and the request named %d", crud.ErrInvalid, len(values))
}

// userNameOf is what a replace claims the address is. A draft that names none is
// the caller saying nothing, which is not a change and is not a refusal.
func userNameOf(in contracts.UserDraft, row userRow) string {
	if v := strings.TrimSpace(in.UserName); v != "" {
		return v
	}
	return row.Email
}

func patchBool(op contracts.Operation) (bool, error) {
	var v bool
	if err := json.Unmarshal(op.Value, &v); err != nil {
		return false, fmt.Errorf("%w: %s takes true or false", crud.ErrInvalid, op.Path)
	}
	return v, nil
}

func patchString(op contracts.Operation) (string, error) {
	var v string
	if err := json.Unmarshal(op.Value, &v); err != nil {
		// An explicit null is the protocol's way of clearing a value, and it
		// arrives as JSON that is not a string.
		if jsonIsNull(op.Value) {
			return "", nil
		}
		return "", fmt.Errorf("%w: %s takes a string", crud.ErrInvalid, op.Path)
	}
	return v, nil
}

func patchMembers(op contracts.Operation) ([]contracts.MemberRef, error) {
	if jsonIsNull(op.Value) {
		return nil, nil
	}
	var direct []contracts.MemberRef
	if err := json.Unmarshal(op.Value, &direct); err == nil {
		return direct, nil
	}
	// The other spelling the protocol allows: one operation naming one member.
	var wrapped struct {
		Value []contracts.MemberRef `json:"value"`
	}
	if err := json.Unmarshal(op.Value, &wrapped); err == nil {
		return wrapped.Value, nil
	}
	var single contracts.MemberRef
	if err := json.Unmarshal(op.Value, &single); err == nil && single.Value != "" {
		return []contracts.MemberRef{single}, nil
	}
	return nil, fmt.Errorf("%w: %s takes a list of members", crud.ErrInvalid, op.Path)
}

// expandOperations reads the way a PATCH operation states what it writes without
// naming it in `path`.
//
// The shape that matters is a pathless replace. Okta deactivates a leaver with
// {"op":"replace","value":{"active":false}} and Microsoft's guide sends the same
// fact as {"op":"replace","path":"active","value":false}; a door that reads only
// the second answers the first with 422 and leaves a person who has left the
// company able to sign in, which is the one request a deprovisioning path may not
// get wrong. An operation with no path is a statement about the resource, so its
// value is read as the set of attributes it writes, one operation per attribute,
// and each then meets the judgement it would have met named in the path — the
// refusals included. A value that is not an object of attributes is refused
// rather than guessed at.
func expandOperations(ops []contracts.Operation) ([]contracts.Operation, error) {
	out := make([]contracts.Operation, 0, len(ops))
	for _, op := range ops {
		if strings.TrimSpace(op.Path) != "" {
			out = append(out, op)
			continue
		}
		var attrs map[string]json.RawMessage
		if err := json.Unmarshal(op.Value, &attrs); err != nil || len(attrs) == 0 {
			return nil, fmt.Errorf("%w: a %s with no path names the attributes it writes in an object", crud.ErrInvalid, op.Op)
		}
		for _, name := range slices.Sorted(maps.Keys(attrs)) {
			out = append(out, contracts.Operation{Op: op.Op, Path: name, Value: attrs[name]})
		}
	}
	return out, nil
}

// memberSelection is a members path read for the one person it names.
//
// members[value eq "<id>"] with no value beside it is how RFC 7644 §3.5.2.2
// removes one member and how Microsoft's provisioning guide tells its customers
// to send it. Reading that path as plain "members" answers it by emptying the
// collection — a standard request met with the destructive answer — so the
// comparison inside the brackets is read and applied. A selection this service
// cannot evaluate is refused rather than dropped, which is the same rule the
// filter on a list stands under: an operation that silently meant something else
// leaves two systems disagreeing about who is in a group.
func memberSelection(path string) (string, error) {
	open := strings.Index(path, "[")
	if open < 0 {
		return "", nil
	}
	fields := strings.Fields(strings.TrimSuffix(path[open+1:], "]"))
	if len(fields) != 3 || fields[0] != "value" || fields[1] != "eq" {
		return "", fmt.Errorf("%w: %q; this service selects one member with value eq \"<id>\" and nothing else", crud.ErrInvalid, path)
	}
	id := strings.Trim(fields[2], `"`)
	if _, err := uuid.Parse(id); err != nil {
		return "", fmt.Errorf("%w: %q selects %q, which is not a person's id", crud.ErrInvalid, path, id)
	}
	return id, nil
}

// jsonIsNull is the protocol's way of clearing a value: an explicit null, and
// the absent value a lenient decoder leaves behind. It clears rather than
// refusing, because a connector that sends "externalId": null is saying it has no
// identifier for this person, which is true and worth recording.
func jsonIsNull(raw json.RawMessage) bool {
	s := strings.TrimSpace(string(raw))
	return s == "" || s == "null"
}
