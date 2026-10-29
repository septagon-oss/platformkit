package internal

import (
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
	"github.com/septagon-oss/platformkit/modules/scim/contracts"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// Service is the SCIM surface over this tenant's people and groups.
//
// It holds three collaborators and no state of its own beyond them, which is what
// lets one value serve a request, a job and an event handler at once. None of the
// three is a thing this module may do for itself: the person is modules/user's
// row, what a role name grants is the auth module's table, and whether the caller
// of a write holds a permission is decided by both of those rows at once. All three
// arrive at composition.
type Service struct {
	users     usercontracts.Provisioning
	roles     contracts.RoleDirectory
	authority contracts.Authority
}

// NewService returns the service. module.go builds it and refuses a composition
// that named neither collaborator.
func NewService(users usercontracts.Provisioning, roles contracts.RoleDirectory, authority contracts.Authority) *Service {
	return &Service{users: users, roles: roles, authority: authority}
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

// close stops a person signing in, and remembers that this door is the one that
// did it. The memory is what lets open below answer a returning person: a
// connector that stood somebody down because they left, and then found their
// account back in the directory, is the ordinary case, and a door that could close
// a person but never reopen them would send that person to an administrator for
// what the connector did to them. An already-closed person is not adopted here:
// whoever closed them did it somewhere else, and this door does not acquire the
// right to undo a decision it did not make.
func (s *Service) close(ctx context.Context, tx db.Tx[db.Tenant], row userRow) error {
	if _, err := s.users.Deactivate(ctx, tx, row.ID); err != nil {
		return err
	}
	if row.Status == usercontracts.StatusInactive {
		return nil
	}
	return rememberClosedByProvider(tx, row.ID, true)
}

// open answers an identity provider that says this person is active.
//
// Two states are `active` at this door and they are not the same fact. An invited
// or unverified person has not been reached at their address yet; ConfirmAddress is
// the command the user module owns for a provider that vouches for it, and it
// refuses the states it refuses — an approval that belongs to a person is not
// performed by an integration.
//
// An inactive person was stood down, and whether this door may stand them back up
// depends on who stood them down. Its own deactivations are reversible here, which
// is what makes PATCH the round trip the protocol means it to be; a person closed
// at the tenant's own screen stays closed, because a credential the tenant handed
// to somebody else's directory is a key for mirroring it, not a way to overrule
// the tenant. Nothing outside this module can tell the two closed states apart,
// so this module keeps the one fact it needs about the people it closed — see
// rememberClosedByProvider.
func (s *Service) open(ctx context.Context, tx db.Tx[db.Tenant], row userRow) error {
	if row.Status != usercontracts.StatusInactive {
		_, err := s.users.ConfirmAddress(ctx, tx, row.ID, row.Email)
		return err
	}
	closed, err := closedByProvider(tx, row.ID)
	if err != nil {
		return err
	}
	if !closed {
		return fmt.Errorf("%w: this person was stopped signing in at the tenant's own door, which is the door that lets them back in",
			crud.ErrConflict)
	}
	if _, err := s.users.Reactivate(ctx, tx, row.ID); err != nil {
		return err
	}
	// The memory is spent: a second connector, or an administrator's stand-down
	// the day after this one, is not covered by a deactivation this door already
	// undid.
	return rememberClosedByProvider(tx, row.ID, false)
}

// closedByProvider reads the door's memory of a person. No row is the answer for
// everybody this door never closed, and it is the same answer as a row that says
// nothing happened.
func closedByProvider(tx db.Tx[db.Tenant], userID uuid.UUID) (bool, error) {
	var held struct {
		ProviderDeactivated bool `gorm:"column:provider_deactivated"`
	}
	err := tx.DB().Table("scim_user_external").Select("provider_deactivated").
		Where("user_id = ? AND tenant_id = ?", userID, db.TenantOf(tx).ID).Take(&held).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("scim: read who closed this person: %w", err)
	}
	return held.ProviderDeactivated, nil
}

// rememberClosedByProvider writes that memory, and the lack of it. It is the one
// module that can: the person's status is modules/user's column, and the reason a
// connector is asking for it back is this module's fact.
//
// Clearing never creates a row. A person this door never closed has no memory to
// forget, and a table of rows recording that nothing happened is a table nothing
// reads.
func rememberClosedByProvider(tx db.Tx[db.Tenant], userID uuid.UUID, closed bool) error {
	tenant := db.TenantOf(tx).ID
	if !closed {
		return tx.DB().Table("scim_user_external").
			Where("user_id = ? AND tenant_id = ?", userID, tenant).
			Update("provider_deactivated", false).Error
	}
	return tx.DB().Table("scim_user_external").Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}},
		DoUpdates: clause.Assignments(map[string]any{"provider_deactivated": true}),
	}).Create(map[string]any{
		"tenant_id": tenant, "user_id": userID, "provider_deactivated": true,
	}).Error
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
	if err := mayBindMapping(ctx, tx, s.authority); err != nil {
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

// jsonIsNull is the protocol's way of clearing a value: an explicit null, and
// the absent value a lenient decoder leaves behind. It clears rather than
// refusing, because a connector that sends "externalId": null is saying it has no
// identifier for this person, which is true and worth recording.
func jsonIsNull(raw json.RawMessage) bool {
	s := strings.TrimSpace(string(raw))
	return s == "" || s == "null"
}
