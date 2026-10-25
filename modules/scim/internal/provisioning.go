package internal

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/scim/contracts"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// mayProvision is the same recheck at the module's own door: every write of a
// collection, and every write of a person, asks it before it reads a row.
//
// A membership is the reason it exists. PatchGroup does not only record that a
// person is in a collection — it recomputes what the tenant's own mapping says that
// membership grants, and calls SetRoles with the answer. So a caller that reached
// these commands by name, past the guard on the HTTP door, would be handing out
// roles with a credential the tenant issued for mirroring names, and the trail
// would show the connector's own actor id doing it. `scim:provision` is the
// permission the tenant grants to its identity provider and to nothing else; a
// caller that carries a narrower scope than that is refused here, by name, in the
// transaction the write would have committed in.
//
// One shape is answered differently from mayBindMapping, and the difference is the
// grant at stake rather than the rule: a caller with no actor on the other side of
// its context is this process's own code, and no actor anywhere was refused or
// allowed by it. Binding a collection to a role is a grant decided in advance for
// people the caller may never mirror, and that one is refused to a nameless write
// whatever it is. Mirroring a collection is what this module exists to be asked;
// the module declares no job and subscribes to no event today, so nothing reaches
// these commands without an actor, and the case is allowed rather than refused so
// that the day a background writer arrives it arrives at a comment that says the
// decision was made here and not discovered.
func (s *Service) mayProvision(ctx context.Context, tx db.Tx[db.Tenant]) error {
	held, shape, err := callerHolds(ctx, tx, s.authority, contracts.PermissionProvision)
	if err != nil {
		return fmt.Errorf("scim: cannot tell whether this caller holds %s: %w", contracts.PermissionProvision, err)
	}
	if held || shape == noCaller {
		return nil
	}
	return fmt.Errorf("%w: provisioning through this door is %s work, and this caller %s",
		tenancy.ErrPolicyDenied, contracts.PermissionProvision, lacksGrant(shape))
}

// callerShape is how the caller of a write arrived, which decides who can say what
// it holds.
type callerShape int

const (
	// noCaller is a write with nobody on the other side of it.
	noCaller callerShape = iota
	// scopedCaller is a credential that carries its own list of grants.
	scopedCaller
	// signedInCaller is a session, whose list is two other modules' rows.
	signedInCaller
)

// callerHolds answers whether the caller holds permission, and says which shape
// asked, so the refusal can name the reason a caller was refused rather than
// describing a shape it was not.
//
// A credential's own list is a ceiling and never an addition, so a caller that
// carries one is answered from it by exact name, whoever it is and however it
// reached this function. A session carries no list: what its roles grant is read
// from rows this module does not own, so the question goes to the composition's
// Authority, which answers it the way the guard on the module's own route does —
// which is the point of the port rather than a comment on it: a caller who walked
// past the guard is refused by the same rule the guard applied, not by a weaker
// copy of it written here.
func callerHolds(ctx context.Context, tx db.Tx[db.Tenant], authority contracts.Authority, permission string) (bool, callerShape, error) {
	p, ok := tenancy.PrincipalFrom(ctx)
	if !ok || p.UserID == uuid.Nil {
		return false, noCaller, nil
	}
	if p.Permissions != nil {
		return slices.Contains(p.Permissions, permission), scopedCaller, nil
	}
	held, err := authority.Holds(ctx, tx, permission)
	return held, signedInCaller, err
}

// lacksGrant is the half of the refusal sentence that depends on the shape.
func lacksGrant(shape callerShape) string {
	if shape == scopedCaller {
		return "carries its own scope without it"
	}
	return "signs in with roles that do not grant it"
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
