package internal_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/scim"
	"github.com/septagon-oss/platformkit/modules/scim/contracts"
	"github.com/septagon-oss/platformkit/modules/user"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// The cases below ask the questions this module was written to answer, against a
// real Postgres, a real tenant transaction and the real user module — not a fake
// of it. A provisioned person is a row modules/user owns, and the whole claim of
// the design is that SCIM writes that row through the same commands the
// workspace door uses; a test that stubbed those commands would test the
// translation and miss the point.

var (
	acme   = tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}
	globex = tenancy.Tenant{ID: uuid.New(), Slug: "globex", Name: "Globex"}
)

// roleBook is this harness's answer to "does this tenant know that role". Role
// definitions are the auth module's table; the scim module may not read it, so a
// composition answers the question and this is what the reference composition
// will look like in miniature.
type roleBook struct{ known []string }

func (r roleBook) HasRole(_ context.Context, _ db.Tx[db.Tenant], name string) (bool, error) {
	return slices.Contains(r.known, name), nil
}

// wiring is one composed module: the user module's service, this module's service
// over it, and the connection they both run on.
type wiring struct {
	svc   contracts.Service
	users usercontracts.Service
	conn  *db.Conn
}

func compose(t *testing.T, known ...string) wiring {
	t.Helper()
	_, conn := dbtest.Schema(t, user.Migrations, scim.Migrations)
	users, _ := user.New(user.Deps{
		Administration: &usercontracts.AdministrationFunc{
			Ask: func(context.Context, db.Tx[db.Tenant]) ([]string, error) { return []string{"admin"}, nil },
		},
		// The reference composition answers this from the Authorizer the API
		// already holds; here it answers yes, because these cases are about
		// membership and roles arriving, not about who may hand them out — which
		// is modules/user's rule and has its own tests.
		Granting: &usercontracts.GrantingFunc{
			Ask: func(context.Context, db.Tx[db.Tenant]) (bool, error) { return true, nil },
		},
	})
	svc, _ := scim.New(scim.Deps{Users: users, Roles: roleBook{known: known}})
	return wiring{svc: svc, users: users, conn: conn}
}

// inTenant runs fn in one transaction of one tenant, and fails the test if it
// returns anything.
func (w wiring) inTenant(t *testing.T, tenant tenancy.Tenant, fn func(context.Context, db.Tx[db.Tenant]) error) {
	t.Helper()
	err := db.Run(tenancy.WithTenant(t.Context(), tenant), w.conn, fn)
	if err != nil && !errors.Is(err, errStop) {
		t.Fatalf("the transaction did not commit: %v", err)
	}
}

// refused runs fn, expects it to fail with the named sentinel, and rolls the
// transaction back whatever fn did — which is the assertion's whole second half:
// a refusal that wrote its row before it refused would still be here.
func (w wiring) refused(t *testing.T, tenant tenancy.Tenant, want error, fn func(context.Context, db.Tx[db.Tenant]) error) error {
	t.Helper()
	got := db.Run(tenancy.WithTenant(t.Context(), tenant), w.conn, fn)
	if got == nil {
		t.Fatalf("the write was accepted, want %v", want)
	}
	if !errors.Is(got, want) {
		t.Fatalf("the write failed with %v, want it to be %v", got, want)
	}
	if !errors.Is(got, errStop) {
		// db.Run rolled the transaction back on the error; say so once, because
		// every case here reads that rollback as the point.
		t.Logf("refused with %v, and its transaction rolled back", got)
	}
	return got
}

// errStop is how a case that finished early ends its transaction without an
// error being a failure.
var errStop = errors.New("scim test: finished")

func outbox(t *testing.T, tx db.Tx[db.Tenant]) []string {
	t.Helper()
	var names []string
	if err := tx.DB().Table("platformkit_outbox").Order("created_at, id").Pluck("name", &names).Error; err != nil {
		t.Fatalf("read the outbox: %v", err)
	}
	return names
}

func TestTheProviderVouchingForAnAddressIsWhatMakesAPersonAbleToSignIn(t *testing.T) {
	w := compose(t)
	ctx := tenancy.WithTenant(t.Context(), acme)
	err := db.Run(ctx, w.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		// The connector's default: a person exists over there, and nothing has
		// been said about their mailbox.
		invited, err := w.svc.CreateUser(ctx, tx, contracts.UserDraft{
			Schemas: []string{contracts.SchemaUser}, UserName: "newcomer@acme.test", DisplayName: "New Comer",
		})
		if err != nil {
			return err
		}
		if invited.Active {
			t.Errorf("a create with no active flag reported active: %v", invited)
		}
		u, err := w.users.Get(ctx, tx, invited.ID)
		if err != nil {
			return err
		}
		if u.Status != usercontracts.StatusInvited {
			t.Errorf("status = %q, want %q: nothing has vouched for this mailbox", u.Status, usercontracts.StatusInvited)
		}

		// The same connector saying it owns the mailbox.
		vouched, err := w.svc.CreateUser(ctx, tx, contracts.UserDraft{
			Schemas: []string{contracts.SchemaUser}, UserName: "ada@acme.test", Active: pointer(true),
		})
		if err != nil {
			return err
		}
		if !vouched.Active {
			t.Error("a create the provider vouched for came back inactive")
		}
		u, err = w.users.Get(ctx, tx, vouched.ID)
		if err != nil {
			return err
		}
		if u.Status != usercontracts.StatusActive {
			t.Errorf("status = %q, want %q", u.Status, usercontracts.StatusActive)
		}
		if u.PasswordHash != "" {
			t.Error("a person provisioned by an identity provider was given a password hash")
		}

		// Both halves published, in this transaction: the invitation, and the
		// verification the confirmation is. Nothing here invented a third event
		// under a scim. name.
		names := strings.Join(outbox(t, tx), ",")
		for _, want := range []string{usercontracts.EventInvited, usercontracts.EventEmailVerified} {
			if !strings.Contains(names, want) {
				t.Errorf("the outbox holds %q, which is missing %s", names, want)
			}
		}
		if strings.Contains(names, "scim.") {
			t.Errorf("the outbox holds %q: this module publishes nothing under its own name", names)
		}
		return errStop
	})
	if !errors.Is(err, errStop) {
		t.Fatalf("%v", err)
	}
}

func TestAGroupCarriesItsRoleOntoItsMembersAndTakesItBackOff(t *testing.T) {
	w := compose(t, "finance")
	ctx := tenancy.WithTenant(t.Context(), acme)
	err := db.Run(ctx, w.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		// A person with a role an administrator gave them by hand. That role is
		// named by no mapping, and must survive every membership write below.
		ada, err := w.users.Invite(ctx, tx, "ada@acme.test", "Ada")
		if err != nil {
			return err
		}
		if ada, err = w.users.SetRoles(ctx, tx, ada.ID, []string{"reader", "editor"}); err != nil {
			return err
		}

		group, err := w.svc.CreateGroup(ctx, tx, contracts.GroupDraft{
			Schemas: []string{contracts.SchemaGroup}, DisplayName: "Finance", Role: "finance",
		})
		if err != nil {
			return err
		}
		if group.Role != "finance" {
			t.Errorf("the group reads back role %q, want finance", group.Role)
		}

		if _, err := w.svc.PatchGroup(ctx, tx, group.ID, []contracts.Operation{{
			Op: "add", Path: "members",
			Value: []byte(`[{"value":"` + ada.ID.String() + `"}]`),
		}}); err != nil {
			return err
		}
		if ada, err = w.users.Get(ctx, tx, ada.ID); err != nil {
			return err
		}
		if !slices.Contains(ada.Roles, "finance") {
			t.Errorf("after joining Finance the person holds %v, which does not include finance", ada.Roles)
		}
		if !slices.Contains(ada.Roles, "reader") || !slices.Contains(ada.Roles, "editor") {
			t.Errorf("the mapping ungranted what a person was given by hand: %v", ada.Roles)
		}

		// Membership reads back from the person's side, read-only.
		fromThePerson, err := w.svc.GetUser(ctx, tx, ada.ID)
		if err != nil {
			return err
		}
		if len(fromThePerson.Groups) != 1 || fromThePerson.Groups[0].Display != "Finance" {
			t.Errorf("the person reports groups %v, want the one collection they joined", fromThePerson.Groups)
		}

		// Leaving takes the mapped role and nothing else.
		if _, err := w.svc.PatchGroup(ctx, tx, group.ID, []contracts.Operation{{
			Op: "remove", Path: "members",
			Value: []byte(`[{"value":"` + ada.ID.String() + `"}]`),
		}}); err != nil {
			return err
		}
		if ada, err = w.users.Get(ctx, tx, ada.ID); err != nil {
			return err
		}
		if slices.Contains(ada.Roles, "finance") {
			t.Errorf("after leaving Finance the person still holds %v", ada.Roles)
		}
		if len(ada.Roles) != 2 {
			t.Errorf("leaving the group left %v, want the two roles the person was given", ada.Roles)
		}
		return errStop
	})
	if !errors.Is(err, errStop) {
		t.Fatalf("%v", err)
	}
}

func TestAMappingToARoleNobodyDeclaredIsRefusedAndWritesNothing(t *testing.T) {
	w := compose(t, "finance")
	ctx := tenancy.WithTenant(t.Context(), acme)
	got := w.refused(t, acme, crud.ErrInvalid, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		group, err := w.svc.CreateGroup(ctx, tx, contracts.GroupDraft{
			Schemas: []string{contracts.SchemaGroup}, DisplayName: "Finance",
		})
		if err != nil {
			return err
		}
		_, err = w.svc.MapGroup(ctx, tx, group.DisplayName, "ghost")
		return err
	})
	if !strings.Contains(got.Error(), "ghost") {
		t.Errorf("the refusal does not name the role it refused: %v", got)
	}
	// The same transaction that refused is the one that created the group, so
	// nothing at all arrived.
	_ = ctx
	_ = db.Run(tenancy.WithTenant(t.Context(), acme), w.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		list, err := w.svc.GroupMappings(ctx, tx)
		if err != nil {
			return err
		}
		if len(list) != 0 {
			t.Errorf("the refused binding is in the table: %+v", list)
		}
		var n int64
		if err := tx.DB().Table("scim_groups").Count(&n).Error; err != nil {
			return err
		}
		if n != 0 {
			t.Errorf("the refused write left %d group(s) behind", n)
		}
		return nil
	})
}

func TestAnAddressIsRefusedRatherThanMoved(t *testing.T) {
	w := compose(t)
	ctx := tenancy.WithTenant(t.Context(), acme)
	var id uuid.UUID
	if err := db.Run(ctx, w.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		u, err := w.svc.CreateUser(ctx, tx, contracts.UserDraft{
			Schemas: []string{contracts.SchemaUser}, UserName: "ada@acme.test", DisplayName: "Ada",
		})
		if err != nil {
			return err
		}
		id = u.ID
		return nil
	}); err != nil {
		t.Fatalf("%v", err)
	}

	w.refused(t, acme, crud.ErrInvalid, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := w.svc.ReplaceUser(ctx, tx, id, contracts.UserDraft{
			Schemas: []string{contracts.SchemaUser}, UserName: "ada@evil.test",
		})
		return err
	})
	w.refused(t, acme, crud.ErrInvalid, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := w.svc.PatchUser(ctx, tx, id, []contracts.Operation{{
			Op: "replace", Path: "userName", Value: []byte(`"ada@evil.test"`),
		}})
		return err
	})
	// A password is refused, and nobody is created in front of the refusal.
	w.refused(t, acme, crud.ErrInvalid, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := w.svc.CreateUser(ctx, tx, contracts.UserDraft{
			Schemas: []string{contracts.SchemaUser}, UserName: "mallory@acme.test", Password: "hunter2",
		})
		return err
	})
	if err := db.Run(ctx, w.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		u, err := w.users.Get(ctx, tx, id)
		if err != nil {
			return err
		}
		if u.Email != "ada@acme.test" {
			t.Errorf("the address moved to %q", u.Email)
		}
		var n int64
		if err := tx.DB().Table("users").Where("email = ?", "mallory@acme.test").Count(&n).Error; err != nil {
			return err
		}
		if n != 0 {
			t.Error("the create that named a password wrote the person anyway")
		}
		return nil
	}); err != nil {
		t.Fatalf("%v", err)
	}
}

func TestAFilterThisServiceDoesNotMakeIsRefusedRatherThanIgnored(t *testing.T) {
	w := compose(t)
	ctx := tenancy.WithTenant(t.Context(), acme)
	if err := db.Run(ctx, w.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		for _, address := range []string{"ada@acme.test", "grace@acme.test"} {
			if _, err := w.svc.CreateUser(ctx, tx, contracts.UserDraft{
				Schemas: []string{contracts.SchemaUser}, UserName: address,
			}); err != nil {
				return err
			}
		}
		all, err := w.svc.ListUsers(ctx, tx, contracts.ListQuery{})
		if err != nil {
			return err
		}
		if all.TotalResults != 2 || len(all.Resources) != 2 {
			t.Errorf("an unfiltered list answered %d of %d, want both people", len(all.Resources), all.TotalResults)
		}
		// The one comparison this service makes, and the answer the whole module
		// would rather give: the one person, not everybody.
		one, err := w.svc.ListUsers(ctx, tx, contracts.ListQuery{Filter: `userName eq "ada@acme.test"`})
		if err != nil {
			return err
		}
		if one.TotalResults != 1 || one.Resources[0].UserName != "ada@acme.test" {
			t.Errorf("userName eq answered %d results, want the one person", one.TotalResults)
		}
		return errStop
	}); !errors.Is(err, errStop) {
		t.Fatalf("%v", err)
	}

	for _, filter := range []string{`displayName eq "Ada"`, `userName co "ada"`, `active eq true`} {
		w.refused(t, acme, crud.ErrInvalid, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			_, err := w.svc.ListUsers(ctx, tx, contracts.ListQuery{Filter: filter})
			return err
		})
	}
}

func TestAnotherTenantCannotReachThisTenantSCIMRows(t *testing.T) {
	w := compose(t, "finance")
	ctx := tenancy.WithTenant(t.Context(), acme)
	var group, person uuid.UUID
	if err := db.Run(ctx, w.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		g, err := w.svc.CreateGroup(ctx, tx, contracts.GroupDraft{
			Schemas: []string{contracts.SchemaGroup}, DisplayName: "Finance", Role: "finance",
		})
		if err != nil {
			return err
		}
		group = g.ID
		u, err := w.svc.CreateUser(ctx, tx, contracts.UserDraft{
			Schemas: []string{contracts.SchemaUser}, UserName: "ada@acme.test", Active: pointer(true),
		})
		if err != nil {
			return err
		}
		person = u.ID
		if _, err := w.svc.PatchGroup(ctx, tx, group, []contracts.Operation{{
			Op: "add", Path: "members", Value: []byte(`[{"value":"` + person.String() + `"}]`),
		}}); err != nil {
			return err
		}
		// Committed, and not rolled back at the end: a case that asks whether
		// another tenant can read these rows has to leave the rows behind, or it
		// proves only that nothing is anywhere.
		return nil
	}); err != nil {
		t.Fatalf("%v", err)
	}

	// Globex shares the connection, the schema and the tables, and is answered as
	// though none of this existed: not their group, and not a membership in it.
	if err := db.Run(tenancy.WithTenant(t.Context(), globex), w.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if _, err := w.svc.GetGroup(ctx, tx, group); !errors.Is(err, crud.ErrNotFound) {
			t.Errorf("another tenant read Acme's group: %v", err)
		}
		if _, err := w.svc.GetUser(ctx, tx, person); !errors.Is(err, crud.ErrNotFound) {
			t.Errorf("another tenant read Acme's person: %v", err)
		}
		if _, err := w.svc.PatchGroup(ctx, tx, group, []contracts.Operation{{
			Op: "add", Path: "members", Value: []byte(`[{"value":"` + person.String() + `"}]`),
		}}); err != nil && !errors.Is(err, crud.ErrNotFound) && !errors.Is(err, crud.ErrInvalid) {
			t.Errorf("another tenant's membership write failed with something other than not being able to find the group or the person: %v", err)
		}
		list, err := w.svc.ListGroups(ctx, tx, contracts.ListQuery{})
		if err != nil {
			return err
		}
		if len(list.Resources) != 0 {
			t.Errorf("another tenant listed %d groups", len(list.Resources))
		}
		return nil
	}); err != nil {
		t.Fatalf("%v", err)
	}

	// And Acme's own person came out holding the role, which is the other half of
	// the same fact: the write that worked was Acme's.
	if err := db.Run(ctx, w.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		u, err := w.users.Get(ctx, tx, person)
		if err != nil {
			return err
		}
		if !slices.Contains(u.Roles, "finance") {
			t.Errorf("the member holds %v", u.Roles)
		}
		return nil
	}); err != nil {
		t.Fatalf("%v", err)
	}
}

func TestDeletingAPersonReservesTheAddressAndLeavesNobodyAbleToSignIn(t *testing.T) {
	w := compose(t)
	ctx := tenancy.WithTenant(t.Context(), acme)
	var id uuid.UUID
	if err := db.Run(ctx, w.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		u, err := w.svc.CreateUser(ctx, tx, contracts.UserDraft{
			Schemas: []string{contracts.SchemaUser}, UserName: "ada@acme.test", Active: pointer(true),
		})
		if err != nil {
			return err
		}
		id = u.ID
		if err := w.svc.DeactivateUser(ctx, tx, id); err != nil {
			return err
		}
		// The protocol's answer to the same create, a day later: the same person
		// back, not a second one.
		if _, err := w.svc.CreateUser(ctx, tx, contracts.UserDraft{
			Schemas: []string{contracts.SchemaUser}, UserName: "ada@acme.test",
		}); !errors.Is(err, crud.ErrConflict) {
			t.Errorf("re-provisioning a deactivated address gave %v, want a conflict that keeps the address reserved", err)
		}
		return errStop
	}); !errors.Is(err, errStop) {
		t.Fatalf("%v", err)
	}
	// A deactivated person is out of every list a connector may act on, and a
	// GET by their id answers the way an address nobody has answers.
	if err := db.Run(ctx, w.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		list, err := w.svc.ListUsers(ctx, tx, contracts.ListQuery{})
		if err != nil {
			return err
		}
		if list.TotalResults != 0 {
			t.Errorf("a deactivated person is still listed: %d", list.TotalResults)
		}
		if _, err := w.svc.GetUser(ctx, tx, id); !errors.Is(err, crud.ErrNotFound) {
			t.Errorf("GET on a deactivated person = %v, want not found", err)
		}
		return nil
	}); err != nil {
		t.Fatalf("%v", err)
	}
}

func TestADeleteRefusesAGroupThatStillHasMembers(t *testing.T) {
	w := compose(t, "finance")
	ctx := tenancy.WithTenant(t.Context(), acme)
	if err := db.Run(ctx, w.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		u, err := w.svc.CreateUser(ctx, tx, contracts.UserDraft{
			Schemas: []string{contracts.SchemaUser}, UserName: "ada@acme.test",
		})
		if err != nil {
			return err
		}
		g, err := w.svc.CreateGroup(ctx, tx, contracts.GroupDraft{
			Schemas: []string{contracts.SchemaGroup}, DisplayName: "Finance", Role: "finance",
			Members: []contracts.MemberRef{{Value: u.ID.String()}},
		})
		if err != nil {
			return err
		}
		if err := w.svc.DeleteGroup(ctx, tx, g.ID); !errors.Is(err, crud.ErrConflict) {
			t.Errorf("deleting a group with members gave %v, want a conflict naming the members still in it", err)
		}
		return errStop
	}); !errors.Is(err, errStop) {
		t.Fatalf("%v", err)
	}
}

func TestTheDiscoveryDocumentsAnswerForThemselves(t *testing.T) {
	w := compose(t)
	ctx := tenancy.WithTenant(t.Context(), acme)
	if err := db.Run(ctx, w.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		cfg, err := w.svc.ProviderConfig(ctx)
		if err != nil {
			return err
		}
		// The document and the router have to agree, because a client configures
		// itself from the document: bulk is off and no /Bulk door is mounted.
		if cfg.Bulk.Supported || cfg.ChangePassword.Supported || cfg.Sort.Supported || cfg.SupportingETag {
			t.Errorf("the document promises a capability the service does not implement: %+v", cfg)
		}
		if !cfg.Patch.Supported || !cfg.Filter.Supported {
			t.Errorf("the document refuses a capability the service does implement: %+v", cfg)
		}
		types, err := w.svc.ResourceTypes(ctx)
		if err != nil {
			return err
		}
		if len(types) != 2 || types[0].Endpoint != "/Users" || types[1].Endpoint != "/Groups" {
			t.Errorf("resource types = %+v, want User at /Users and Group at /Groups", types)
		}
		if _, err := w.svc.ResourceType(ctx, "Membership"); !errors.Is(err, crud.ErrNotFound) {
			t.Errorf("a third resource type was answered with %v, want not found", err)
		}
		schemas, err := w.svc.Schemas(ctx)
		if err != nil {
			return err
		}
		if len(schemas) != 2 {
			t.Errorf("%d schemas, want User and Group", len(schemas))
		}
		// The readOnly attributes are the module's explanation of itself, so the
		// ones that matter are named here rather than trusted.
		user := schemas[0]
		for _, want := range []struct{ name, mutability string }{
			{"userName", "readOnly"}, {"password", "readOnly"}, {"groups", "readOnly"}, {"emails", "readOnly"},
			{"active", "readWrite"}, {"externalId", "readWrite"},
		} {
			var found bool
			for _, a := range user.Attributes {
				if a.Name != want.name {
					continue
				}
				found = true
				if a.Mutability != want.mutability {
					t.Errorf("attribute %s says %q, want %q", want.name, a.Mutability, want.mutability)
				}
			}
			if !found {
				t.Errorf("the User schema names no %s attribute", want.name)
			}
		}
		return errStop
	}); !errors.Is(err, errStop) {
		t.Fatalf("%v", err)
	}
}

func pointer(v bool) *bool { return &v }
