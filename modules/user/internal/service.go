// Package internal is every implementation of the user module. Nothing outside
// modules/user can import it, which is the compiler enforcing idea 3: a
// consumer takes contracts.Service, and taking anything else does not build.
package internal

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/gorm"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/user/contracts"
)

// Service is the user lifecycle. Everything a command needs arrives with the
// transaction it is given, which is what lets one instance serve a request, a
// job and an event handler at once; the one field is the question this module
// cannot answer for itself.
type Service struct {
	// locales answers which languages the tenant of the transaction is served in.
	// That set is the tenant module's fact and this module reads no table it does
	// not own, so composition supplies the answer. See contracts.LocalePolicy.
	locales contracts.LocalePolicy
	// administering answers which of this tenant's roles can change a role
	// again. That answer is the auth module's table and this module must not
	// read it, so the application supplies it. See contracts.Administration
	// and floor.
	administering contracts.Administration
	// granting answers whether the caller may hand out one of those roles. See
	// contracts.Granting: without it, the roles door is one anybody who may write
	// a user may walk through.
	granting contracts.Granting
}

// NewService returns the lifecycle commands. module.go constructs it, and
// module.go is where a composition with no Administration, no Granting or no
// LocalePolicy is refused.
func NewService(administering contracts.Administration, granting contracts.Granting, locales contracts.LocalePolicy) *Service {
	return &Service{administering: administering, granting: granting, locales: locales}
}

var _ contracts.Service = (*Service)(nil)

// SetPreferredLocale is one person saying which language their pages are written
// in; an empty tag takes that back and leaves the answer to the browser and the
// tenant's default.
//
// Three things make it a command rather than a field on a PATCH. It publishes
// user.language_set, so "when did they ask for Portuguese, and what did they ask
// for before" is answerable from the trail. Its argument is checked against the
// set the tenant is served in at the moment the write lands — a form's select box
// is a picture of that set as the page was drawn, and an operator can narrow it
// while the page sits open, so the alternative is storing a language nobody serves
// and letting the person wonder. And nothing in its arguments names a person: the
// row is the principal's own, which is the difference between a preference and a
// way to edit a colleague. That is enforced by construction rather than by a
// comparison — the command reads who is asking from the transaction, so there is
// no id here to lie about, and the tenant scope comes from the same transaction
// row-level security already enforces.
//
// The write itself is choose; see it for the lock order and for what an expected
// revision changes.
func (s *Service) SetPreferredLocale(ctx context.Context, tx db.Tx[db.Tenant], tag string) (*contracts.User, error) {
	return s.choose(ctx, tx, nil, tag)
}

// SetPreferredLocaleAt is the same command for the caller that read the choice
// first. The versioned account door — GET then PUT /api/v1/user/me/language — is
// that caller: it drew a page at one revision, and a second tab, an operator who
// narrowed the set, or the person's own other device may have moved it since. The
// row lock below is what makes the comparison and the write one decision rather
// than a promise about the row between them.
func (s *Service) SetPreferredLocaleAt(ctx context.Context, tx db.Tx[db.Tenant], expectedRevision int64, tag string) (*contracts.User, error) {
	return s.choose(ctx, tx, &expectedRevision, tag)
}

// choose is the one decision, reached with or without an expected revision.
//
// A nil expectation says the caller brought an intent and not a read: "answer me
// in Portuguese" is a complete statement, and the losing half of two such writes
// is told which language it now holds by the row it gets back. An expectation says
// the caller read something, and the only honest answer to a read that is no longer
// current is a conflict that writes nothing and publishes nothing.
//
// The tenant's row is locked before the person's, the order every write that
// touches both takes: SetLocale changes the set this tag has to be found in, and
// two writers who take the two rows in the opposite order deadlock rather than
// decide. Holding it is also what makes the read below and the write below one
// decision — no other transaction can move either row between them, which is what
// makes the revision compared below the revision the write lands on.
func (s *Service) choose(ctx context.Context, tx db.Tx[db.Tenant], expected *int64, tag string) (*contracts.User, error) {
	principal, signedIn := tenancy.PrincipalFrom(ctx)
	if !signedIn || principal.UserID == uuid.Nil {
		return nil, contracts.ErrNoSignIn
	}
	languages, err := s.locales.CurrentLanguages(ctx, tx)
	if err != nil {
		return nil, err
	}
	if tag != "" && !slices.Contains(languages.Preferred(), tag) {
		return nil, fmt.Errorf("%w: %s is not a language this tenant is served in", crud.ErrInvalid, tag)
	}
	u, err := crud.GetForUpdate[*contracts.User](tx, principal.UserID)
	if err != nil {
		return nil, err
	}
	if u.PreferredLocale == tag {
		// The choice they already hold is not a change: no write, no event, and no
		// revision anybody has to read again.
		return u, nil
	}
	if expected != nil && u.LocaleRevision != *expected {
		return nil, fmt.Errorf("%w: this choice was read at revision %d and this person's is on revision %d",
			crud.ErrConflict, *expected, u.LocaleRevision)
	}
	was := u.PreferredLocale
	u.PreferredLocale, u.LocaleRevision = tag, u.LocaleRevision+1
	if err := crud.Update(ctx, tx, u, "preferred_locale", "locale_revision", "updated_at"); err != nil {
		return nil, err
	}
	// The row and its event are one decision. An outbox that refuses the event
	// refuses the choice with it — a preference nobody will ever be told about is
	// a preference that was never made — and the command hands back no row, so no
	// caller can read a change that did not commit.
	if err := events.Publish(ctx, tx, contracts.EventLanguageSet, contracts.LanguageSet{
		UserID: u.ID, Was: was, Now: tag, At: db.Now(),
	}); err != nil {
		return nil, err
	}
	return u, nil
}

// OwnLanguage is the account door's read: the choice and the revision to bring
// back with the next one. It names no person because it answers the one the
// transaction is standing for, and it takes no lock — a read a caller may hold
// open while it draws a page is not the read that decides a write.
func (s *Service) OwnLanguage(ctx context.Context, tx db.Tx[db.Tenant]) (contracts.Language, error) {
	principal, signedIn := tenancy.PrincipalFrom(ctx)
	if !signedIn || principal.UserID == uuid.Nil {
		return contracts.Language{}, contracts.ErrNoSignIn
	}
	var row struct {
		Locale   string
		Revision int64
	}
	if err := tx.DB().Table("users").Select("preferred_locale AS locale, locale_revision AS revision").
		Where("id = ?", principal.UserID).Take(&row).Error; err != nil {
		return contracts.Language{}, crud.Classify(err)
	}
	return contracts.Language{Locale: row.Locale, Revision: row.Revision}, nil
}

// Invite creates a user with no password and publishes the invitation. Auth's
// subscriber sends the link through the delivery capability the app supplies;
// redeeming it lets the recipient choose their password.
func (s *Service) Invite(ctx context.Context, tx db.Tx[db.Tenant], email, displayName string) (*contracts.User, error) {
	u := &contracts.User{Email: email, DisplayName: displayName, Status: contracts.StatusInvited}
	if err := crud.Create(ctx, tx, u); err != nil {
		return nil, err
	}
	return u, events.Publish(ctx, tx, contracts.EventInvited, contracts.Invited{
		UserID: u.ID, Email: u.Email, Status: u.Status, At: db.Now(),
	})
}

// SetPassword hashes and stores a password, and makes an invited user active.
//
// It is not idempotent and must not be: setting a password to the value it
// already had is still a password change, and a person who did it deliberately
// has to see it in their own audit trail.
func (s *Service) SetPassword(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, password string) error {
	u, err := lockedUser(tx, id)
	if err != nil {
		return err
	}
	if u.Status != contracts.StatusInvited && u.Status != contracts.StatusActive {
		return fmt.Errorf("%w: only invited or active users can be given a password", crud.ErrConflict)
	}
	hash, err := contracts.HashPassword(password)
	if err != nil {
		return fmt.Errorf("%w: %s", crud.ErrInvalid, err)
	}
	u.PasswordHash, u.Status = hash, contracts.StatusActive
	// The columns this changed, and no others: a whole-row write would put
	// every field back to what this transaction read, losing a concurrent
	// change to a field it never touched.
	if err := crud.Update(ctx, tx, u, "password_hash", "status", "updated_at"); err != nil {
		return err
	}
	return events.Publish(ctx, tx, contracts.EventPasswordSet, contracts.PasswordSet{UserID: u.ID, At: db.Now()})
}

// SetRoles replaces the roles this user holds. The same set again — in any
// order — changes nothing and publishes nothing: a retried click must not
// appear twice in an audit of who was made an administrator.
//
// Taking the last administrator's roles away is refused, because afterwards
// nobody in the tenant could grant them back. See floor.
func (s *Service) SetRoles(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, roles []string) (*contracts.User, error) {
	// Locked, and not the plain read it was. The floor below decides on what
	// this row holds today, so a concurrent write to it between the read and
	// the update is the decision being made about a row that no longer exists
	// in that form — and two clicks on one person's roles would also have lost
	// one of them outright.
	u, err := lockedUser(tx, id)
	if err != nil {
		return nil, err
	}
	want := normalise(roles)
	was := slices.Clone([]string(u.Roles))
	if slices.Equal(was, want) {
		return u, nil
	}
	// Promoting somebody is a different act from editing them, and it is guarded
	// as one: see contracts.Granting. Only the roles this write *adds* are
	// asked about, and only the administering ones, so a refusal here can never
	// be the reason a tenant cannot take a role back.
	// The grant question first, whoever the caller is: see
	// contracts.Granting's note about the caller that is not a person, which is a
	// composition acting and not a promotion somebody asked for.
	if added, err := s.addedAdministering(ctx, tx, was, want); err != nil {
		return nil, err
	} else if len(added) > 0 {
		// The decision is asked once, of the port, and only for a write that
		// really promotes somebody: a query before every save would price the
		// ordinary edit of a name for a rule about power.
		may, err := s.granting.May(ctx, tx)
		if err != nil {
			return nil, fmt.Errorf("%w: %s", crud.ErrInvalid, err)
		}
		if !may {
			return nil, contracts.RefuseUngrantable(added[0])
		}
	}
	after := *u
	after.Roles = want
	if err := s.floor(ctx, tx, u, &after); err != nil {
		return nil, err
	}
	u.Roles = want
	if err := crud.Update(ctx, tx, u, "roles", "updated_at"); err != nil {
		return nil, err
	}
	return u, events.Publish(ctx, tx, contracts.EventRolesSet, contracts.RolesSet{
		UserID: u.ID, Was: was, Now: want, At: db.Now(),
	})
}

// Holders is contracts.Service.Holders: CanAdminister as one statement, under
// the tenant's own row-level policy — which is the tenant boundary here, not a
// WHERE clause this package remembered to write.
//
// It is the same predicate otherAdministrators writes for the floor, and the
// reason both exist is that the floor wants the rows and this wants the ids.
func (s *Service) Holders(ctx context.Context, tx db.Tx[db.Tenant], roles []string) ([]uuid.UUID, error) {
	if len(roles) == 0 {
		// "Nothing was named" is not "everybody".
		return nil, nil
	}
	var ids []uuid.UUID
	err := tx.DB().Model(&contracts.User{}).
		Where("deleted_at IS NULL AND status = ? AND roles && ?::text[]",
			contracts.StatusActive, pq.StringArray(roles)).
		Order("id").Pluck("id", &ids).Error
	if err != nil {
		return nil, fmt.Errorf("user: look for the holders of a role: %w", err)
	}
	return ids, nil
}

// addedAdministering is the roles this write would give a person they do not
// already hold, restricted to the ones that administer the tenant. Empty means
// the write grants nobody anything, and the Granting port is never asked.
func (s *Service) addedAdministering(ctx context.Context, tx db.Tx[db.Tenant], was, want []string) ([]string, error) {
	administering, err := s.administering.Administering(ctx, tx)
	if err != nil {
		return nil, err
	}
	var added []string
	for _, role := range want {
		if !slices.Contains(was, role) && slices.Contains(administering, role) {
			added = append(added, role)
		}
	}
	return added, nil
}

// SetHandle claims or renames a handle. See contracts.Service.SetHandle.
//
// The write goes through crud.Update, so a lost race against another claimant
// surfaces as the unique index users_tenant_handle and kit/crud maps that to
// ErrConflict rather than a 500 — the pre-check below is for the good message, not
// for correctness.
func (s *Service) SetHandle(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, handle string) (*contracts.User, error) {
	u, err := crud.Get[*contracts.User](tx, id)
	if err != nil {
		return nil, err
	}
	want := strings.ToLower(strings.TrimSpace(handle))
	if want == "" {
		return nil, fmt.Errorf("%w: a handle cannot be released once claimed", crud.ErrInvalid)
	}
	if u.Handle == want {
		return u, nil
	}
	if contracts.ReservedHandle(want) {
		return nil, fmt.Errorf("%w: handle %q names the platform or a role, not a person", crud.ErrInvalid, want)
	}
	if taken, err := s.ByHandle(ctx, tx, want); err == nil && taken.ID != u.ID {
		// The name of the rule, not the name of the person: "ada is taken" tells a
		// probe nothing, "ada is Ada Lovelace" tells them who is here.
		return nil, fmt.Errorf("%w: that handle is already claimed in this tenant", crud.ErrConflict)
	}
	was := u.Handle
	u.Handle = want
	if err := crud.Update(ctx, tx, u, "handle", "updated_at"); err != nil {
		return nil, err
	}
	return u, events.Publish(ctx, tx, contracts.EventHandleSet, contracts.HandleSet{
		UserID: u.ID, Was: was, Now: want, At: db.Now(),
	})
}

// ByHandle is the human lookup. See contracts.Service.ByHandle.
func (s *Service) ByHandle(_ context.Context, tx db.Tx[db.Tenant], handle string) (*contracts.User, error) {
	var u contracts.User
	err := tx.DB().Where("lower(handle) = ? AND deleted_at IS NULL",
		strings.ToLower(strings.TrimSpace(handle))).Take(&u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, crud.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("user: find by handle: %w", err)
	}
	return &u, nil
}

// Deactivate stops the user signing in. Deactivating them again changes nothing.
//
// Deactivating the last administrator is refused, because afterwards nobody in
// the tenant could activate anybody. See floor.
func (s *Service) Deactivate(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*contracts.User, error) {
	u, err := lockedUser(tx, id)
	if err != nil {
		return nil, err
	}
	if u.Status == contracts.StatusInactive {
		return u, nil
	}
	after := *u
	after.Status = contracts.StatusInactive
	if err := s.floor(ctx, tx, u, &after); err != nil {
		return nil, err
	}
	u.Status = contracts.StatusInactive
	if err := crud.Update(ctx, tx, u, "status", "updated_at"); err != nil {
		return nil, err
	}
	return u, events.Publish(ctx, tx, contracts.EventDeactivated, contracts.Deactivated{UserID: u.ID, At: db.Now()})
}

// Get is one user of this tenant.
func (s *Service) Get(_ context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*contracts.User, error) {
	return crud.Get[*contracts.User](tx, id)
}

// ByEmail is the login lookup. The comparison is lower(email), which is the
// expression the unique index in migrations/000007 is built on, so this is an
// index scan and not a sequential one.
func (s *Service) ByEmail(_ context.Context, tx db.Tx[db.Tenant], email string) (*contracts.User, error) {
	var u contracts.User
	err := tx.DB().Where("lower(email) = ? AND deleted_at IS NULL",
		strings.ToLower(strings.TrimSpace(email))).Take(&u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, crud.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("user: find by email: %w", err)
	}
	return &u, nil
}

// Provision creates a user in a named tenant, from a transaction that belongs to
// no tenant. See contracts.Service.Provision.
//
// An empty password makes an invited user rather than an active one, and that
// is the whole of the second caller: the control plane creates a tenant and has
// to give it a first administrator, in the same cross-tenant transaction, and
// an operator who had to choose somebody else's password to do it would be an
// operator who knows it. The invitation event that follows is what mails them a
// link. The bootstrap passes a password and gets the active user it prints.
func (s *Service) Provision(ctx context.Context, tx db.Tx[db.System], tenantID uuid.UUID,
	email, displayName, password string, roles []string,
) (*contracts.User, error) {
	status, hash := contracts.StatusInvited, ""
	if password != "" {
		var err error
		if hash, err = contracts.HashPassword(password); err != nil {
			return nil, fmt.Errorf("%w: %s", crud.ErrInvalid, err)
		}
		status = contracts.StatusActive
	}
	at := db.Now()
	u := &contracts.User{
		Email: email, DisplayName: displayName, Status: status,
		Roles: normalise(roles), PasswordHash: hash,
	}
	u.ID, u.TenantID, u.CreatedAt, u.UpdatedAt = uuid.New(), tenantID, at, at
	if err := u.Validate(ctx); err != nil {
		return nil, fmt.Errorf("%w: %s", crud.ErrInvalid, err)
	}
	if err := tx.DB().Create(u).Error; err != nil {
		return nil, fmt.Errorf("user: provision %s: %w", u.Email, err)
	}
	return u, events.PublishFor(ctx, tx, tenantID, contracts.EventInvited, contracts.Invited{
		UserID: u.ID, Email: u.Email, Status: u.Status, At: at,
	})
}

// normalise is the stored form of a role set: trimmed, lower-cased, deduplicated
// and sorted. Sorted, so that "the same roles in another order" is the same
// value and SetRoles can tell that nothing changed.
func normalise(roles []string) contracts.Roles {
	out := make(contracts.Roles, 0, len(roles))
	for _, r := range roles {
		if r = strings.ToLower(strings.TrimSpace(r)); r != "" && !slices.Contains(out, r) {
			out = append(out, r)
		}
	}
	slices.Sort(out)
	return out
}
