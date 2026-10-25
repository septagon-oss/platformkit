package internal

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm/clause"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/scim/contracts"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

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
	if err := s.mayProvision(ctx, tx); err != nil {
		return nil, err
	}
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
	if err := s.mayProvision(ctx, tx); err != nil {
		return nil, err
	}
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
	if err := s.mayProvision(ctx, tx); err != nil {
		return nil, err
	}
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
	if err := s.mayProvision(ctx, tx); err != nil {
		return err
	}
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
// See callerHolds for how a caller is read and answered. A refusal reads nothing,
// writes nothing, publishes nothing and returns no row, and it arrives as the
// kernel's own 403 for a caller that reached it through a request. Unlike
// mayProvision, this one refuses a write that names no caller at all: binding a
// collection to a role is a grant decided in advance for people the caller may
// never mirror, and a rule with a bypass one forgotten context value deep is the
// hole this function exists to close.
func mayBindMapping(ctx context.Context, tx db.Tx[db.Tenant], authority contracts.Authority) error {
	held, shape, err := callerHolds(ctx, tx, authority, usercontracts.PermissionUserManage)
	if err != nil {
		return fmt.Errorf("scim: cannot tell whether this caller holds %s: %w", usercontracts.PermissionUserManage, err)
	}
	if held {
		return nil
	}
	if shape == noCaller {
		return fmt.Errorf("%w: binding a group to a role is %s work, and this write names no caller who holds it",
			tenancy.ErrPolicyDenied, usercontracts.PermissionUserManage)
	}
	return fmt.Errorf("%w: binding a group to a role is %s work, and this caller %s",
		tenancy.ErrPolicyDenied, usercontracts.PermissionUserManage, lacksGrant(shape))
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
