package admin_test

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/admin"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/ui/resource"
)

// rolesPerPage is what the screen must page at: ui/resource's screenful, the
// same number every generated list uses.
const rolesPerPage = resource.PerPage

// roleStore is the auth module's role administration, in memory. It keeps the
// two rules the real one is tested on separately — a role names a permission
// the composition defines, and an operator permission belongs to the operator's
// own tenant — because the screen's job is to offer only what the write accepts,
// and a stand-in that accepted anything could not show that it does.
type roleStore struct {
	roles []*authcontracts.Role
	// operator is the tenant the store believes it is writing in. The real
	// SetRole reads it off the transaction; a fake with no transaction is told.
	operator bool
	// fail is what the read answers with, for the one case the page cannot
	// render around.
	fail error
}

func (s *roleStore) Roles(context.Context, db.Tx[db.Tenant]) ([]*authcontracts.Role, error) {
	return s.roles, s.fail
}

func (s *roleStore) SetRole(_ context.Context, _ db.Tx[db.Tenant], name string,
	permissions []string, declared []tenancy.Grant) (*authcontracts.Role, error) {
	name, err := authcontracts.ValidRoleName(name)
	if err != nil {
		return nil, err
	}
	granted, err := authcontracts.CheckedPermissions(permissions, declared,
		tenancy.Tenant{Slug: "acme", Operator: s.operator})
	if err != nil {
		return nil, err
	}
	var was authcontracts.Permissions
	if i := slices.IndexFunc(s.roles, func(r *authcontracts.Role) bool { return r.Name == name }); i >= 0 {
		was = s.roles[i].Grants
	}
	err = authcontracts.CheckedAdministration(name, was, granted,
		func() ([]*authcontracts.Role, error) { return s.roles, nil })
	if err != nil {
		return nil, err
	}
	for _, role := range s.roles {
		if role.Name == name {
			role.Grants = granted
			return role, nil
		}
	}
	role := &authcontracts.Role{Name: name, Grants: granted}
	s.roles = append(s.roles, role)
	return role, nil
}

// withRoles composes the shell over a store seeded with the two roles every
// tenant starts with.
func withRoles(store *roleStore) func(*admin.Deps) {
	return func(d *admin.Deps) { d.Roles = store }
}

func seeded() *roleStore {
	return &roleStore{roles: []*authcontracts.Role{
		{Name: authcontracts.RoleAdmin, Grants: authcontracts.Permissions{authcontracts.Wildcard}},
		{Name: authcontracts.RoleMember, Grants: nil},
	}}
}

// TestTheRolesScreenServesWhatTheAuthModulesNavEntryNames is the defect this
// file exists for: modules/auth declares a nav entry at /admin/auth/roles and
// nothing answered it, so every installation logged a warning at boot and every
// operator saw a menu item that led nowhere.
//
// The screen is the two JSON routes with a person in front of them: what each
// role grants, as ticks, and one button that writes the whole list back —
// which is the shape of the PUT underneath.
func TestTheRolesScreenServesWhatTheAuthModulesNavEntryNames(t *testing.T) {
	store := seeded()
	router := mountAs(t, caller{}, withRoles(store))

	code, body, _ := call(t, router, http.MethodGet, "/admin/auth/roles", "")
	if code != http.StatusOK {
		t.Fatalf("the roles screen = %d %s", code, body)
	}
	for _, want := range []string{">admin<", ">member<", "everything in this tenant"} {
		if !strings.Contains(body, want) {
			t.Errorf("the screen does not show %q", want)
		}
	}
	// Every permission the composition declares is a box, and the ones a role
	// already holds are ticked. admin holds the wildcard and nothing else, so
	// exactly one box in its form is checked.
	for _, want := range []string{`value="note:read"`, `value="note:write"`, `value="plan:read"`, `value="*"`} {
		if !strings.Contains(body, want) {
			t.Errorf("the screen offers no box for %s", want)
		}
	}
	if !strings.Contains(body, `id="pk-role-admin-everything"`) {
		t.Error("the wildcard box has no identity of its own")
	}
	// The screen is guarded by role:manage and posts to itself, so a person who
	// can open it can change what a role grants without reaching for curl.
	if !strings.Contains(body, `action="/admin/auth/roles"`) {
		t.Error("the screen renders no form that writes")
	}
}

// TestTheRolesScreenWritesThroughTheModulesOwnRules is the half that matters
// for authorization: the screen is a door onto SetRole and refuses exactly what
// SetRole refuses, in the page rather than on a fault screen.
func TestTheRolesScreenWritesThroughTheModulesOwnRules(t *testing.T) {
	store := seeded()
	router := mountAs(t, caller{}, withRoles(store))

	code, body, location := call(t, router, http.MethodPost, "/admin/auth/roles",
		"name=member&permissions=note%3Aread&permissions=plan%3Aread")
	if code != http.StatusSeeOther || location != "/admin/auth/roles" {
		t.Fatalf("saving a role = %d to %q: %s", code, location, body)
	}
	member := slices.IndexFunc(store.roles, func(r *authcontracts.Role) bool { return r.Name == "member" })
	if got := []string(store.roles[member].Grants); !slices.Equal(got, []string{"note:read", "plan:read"}) {
		t.Errorf("member grants %v, want the two that were ticked", got)
	}

	// A role with nothing ticked grants nothing. An empty list is a value here:
	// a form that dropped it would make "take everything away" impossible to
	// express, and member is a role that deliberately grants nothing.
	if code, body, _ = call(t, router, http.MethodPost, "/admin/auth/roles", "name=member"); code != http.StatusSeeOther {
		t.Fatalf("emptying a role = %d %s", code, body)
	}
	if got := store.roles[member].Grants; len(got) != 0 {
		t.Errorf("member still grants %v after every box was cleared", got)
	}

	// A new name creates the role, because the route underneath is an upsert
	// and a second door would be a second way to do one write.
	if code, body, _ = call(t, router, http.MethodPost, "/admin/auth/roles",
		"name=editor&permissions=note%3Awrite"); code != http.StatusSeeOther {
		t.Fatalf("creating a role = %d %s", code, body)
	}
	if !slices.ContainsFunc(store.roles, func(r *authcontracts.Role) bool { return r.Name == "editor" }) {
		t.Error("a name nobody had did not become a role")
	}

	// And a refusal comes back as the screen with the message on it, not as the
	// shell's fault page: whoever mistyped a permission is two ticks from
	// getting it right and must not lose the form to find out.
	code, refused, _ := call(t, router, http.MethodPost, "/admin/auth/roles",
		"name=editor&permissions=note%3Aeat")
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("an undeclared permission = %d, want 422: %s", code, refused)
	}
	if !strings.Contains(refused, "note:eat") {
		t.Errorf("the refusal does not name the permission: %s", refused)
	}
	// The message a person can act on, not the package prefix kit/crud puts in
	// front of it — the same reading kit/rest gives a generated form's refusal.
	if strings.Contains(refused, "crud: invalid") {
		t.Errorf("the refusal shows kit/crud's own prefix: %s", refused)
	}
	if !strings.Contains(refused, `action="/admin/auth/roles"`) {
		t.Error("a refusal replaced the screen instead of annotating it")
	}
}

// TestTheRolesScreenOffersOnlyPermissionsThisTenantMayName is the boundary
// docs/adr/0006 draws, at the one place a screen could quietly cross it: an
// operator permission in a customer's tenant is refused by SetRole and by the
// kernel, so a box for one is a control whose only outcome is a 422.
func TestTheRolesScreenOffersOnlyPermissionsThisTenantMayName(t *testing.T) {
	router := mountAs(t, caller{}, withRoles(seeded()))

	_, customer, _ := call(t, router, http.MethodGet, "/admin/auth/roles", "")
	if strings.Contains(customer, `value="plan:write"`) {
		t.Error("a customer's tenant is offered an operator permission")
	}
	if !strings.Contains(customer, `value="plan:read"`) {
		t.Error("a customer's tenant lost the ordinary permissions too")
	}

	_, installation, _ := callAt(t, router, operatorHost, http.MethodGet, "/admin/auth/roles", "")
	if !strings.Contains(installation, `value="plan:write"`) {
		t.Error("the operator's own tenant cannot name its own permission")
	}
}

// TestEveryControlOnTheRolesScreenHasOneIdentity is the defect several forms on
// one page invite: every box is named "permissions", so without an id of its
// own each label would point at whichever input the browser found first, and a
// person ticking member's boxes would be ticking admin's.
func TestEveryControlOnTheRolesScreenHasOneIdentity(t *testing.T) {
	router := mountAs(t, caller{}, withRoles(seeded()))
	_, body, _ := call(t, router, http.MethodGet, "/admin/auth/roles", "")

	seen := map[string]int{}
	for _, match := range regexp.MustCompile(`id="([^"]+)"`).FindAllStringSubmatch(body, -1) {
		seen[match[1]]++
	}
	for id, n := range seen {
		if n > 1 {
			t.Errorf("the identity %q is on %d elements", id, n)
		}
	}
	if seen["pk-role-member-note-read"] == 0 {
		t.Error("a role's boxes are not named after the role they belong to")
	}
}

// TestTheRolesScreenDeclaresTheStatusesItAnswers keeps the API document honest
// about a page. Both routes read, so both can answer what kit/rest maps a
// failed read to; the write can also refuse what was sent. A route that
// declares fewer statuses than it answers is a generated client that treats a
// refusal it was never told about as a transport failure.
func TestTheRolesScreenDeclaresTheStatusesItAnswers(t *testing.T) {
	api, _ := mountWithAPI(t, caller{}, withRoles(seeded()))
	want := map[string][]int{
		"admin-roles":    {http.StatusNotFound, http.StatusServiceUnavailable},
		"admin-role-set": {http.StatusNotFound, http.StatusUnprocessableEntity, http.StatusServiceUnavailable},
	}
	seen := map[string]bool{}
	for _, op := range api.Recorded() {
		expected, named := want[op.OperationID]
		if !named {
			continue
		}
		seen[op.OperationID] = true
		// Containment, not equality: huma adds the statuses it answers for
		// itself — 422 for a parameter it could not read, 500 for the rest —
		// and those are the kernel's to declare, not this page's.
		for _, status := range expected {
			if !slices.Contains(op.Errors, status) {
				t.Errorf("%s declares %v, which does not include %d", op.OperationID, op.Errors, status)
			}
		}
	}
	for id := range want {
		if !seen[id] {
			t.Errorf("the composition recorded no operation %q", id)
		}
	}
}

// TestTheRolesScreenSaysWhatSavingWouldDrop is the state internal.Undeclared
// exists for, seen from the screen: a role holding a permission no module
// declares any more, because the module that declared it left the composition.
// The form has no control for it and the write replaces the whole list, so
// saving destroys it. Hiding it and then deleting it is the worst of the three
// things this screen could do.
func TestTheRolesScreenSaysWhatSavingWouldDrop(t *testing.T) {
	store := seeded()
	store.roles = append(store.roles, &authcontracts.Role{
		Name: "legacy", Grants: authcontracts.Permissions{"note:read", "ghost:read"}})
	router := mountAs(t, caller{}, withRoles(store))

	_, body, _ := call(t, router, http.MethodGet, "/admin/auth/roles", "")
	if strings.Contains(body, `value="ghost:read"`) {
		t.Error("the screen offers a box for a permission no module declares")
	}
	if !strings.Contains(body, "Saving legacy also drops ghost:read") {
		t.Errorf("the screen does not say what saving would destroy: %s", body)
	}
	// And only the role that holds it is warned about: the notice belongs to the
	// form whose button would do it.
	if n := strings.Count(body, "also drops"); n != 1 {
		t.Errorf("%d roles are warned about, want the one that holds it", n)
	}
}

// TestTheRolesScreenRefusesToBrickTheTenant is the write that has no way back.
// Unticking the wildcard on the only role that can administer roles and
// pressing save is one click; afterwards nobody in the tenant can change a role
// again, through this screen or through the JSON route, and the installation's
// operator cannot either — their session does not cross into a customer's
// tenant. The rule is the auth module's, so both doors refuse it; this is the
// screen showing why rather than answering 303 and going quiet.
func TestTheRolesScreenRefusesToBrickTheTenant(t *testing.T) {
	store := seeded()
	router := mountAs(t, caller{}, withRoles(store))

	code, refused, location := call(t, router, http.MethodPost, "/admin/auth/roles", "name=admin")
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("emptying the last administering role = %d to %q, want 422", code, location)
	}
	if !strings.Contains(refused, "last role that grants role:manage") {
		t.Errorf("the refusal does not say what it is protecting: %s", refused)
	}
	if got := store.roles[0].Grants; !slices.Equal([]string(got), []string{authcontracts.Wildcard}) {
		t.Errorf("admin grants %v after a refused write, want the wildcard it had", got)
	}
	// What this case deliberately does not assert is the way around it. Creating
	// a role that grants role:manage and then emptying admin is two clicks, both
	// allowed, and it reaches the same locked-out tenant — because the floor
	// counts roles and cannot see that nobody holds the new one. An assertion
	// here that the second click answers 303 would be this repository writing
	// the escape hatch down as intended behaviour, where the next review would
	// read it as settled. It is a gap, it is named in
	// contracts.CheckedAdministration and in the CHANGELOG, and closing it
	// needs the user module. The conformance suite pins the rule's boundary,
	// which is a statement about the rule and not about the product.
}

// TestTheRolesScreenPagesLikeEveryOtherList is the amplification a hand-written
// list invites. One form per role, one control per declared permission, and
// kit/httpx buffers the whole body until the transaction commits: a tenant that
// creates roles through this very screen — one POST each, and nothing caps them
// — turns a few hundred kilobytes of rows into tens of megabytes of HTML in a
// shared process. Every generated list screen pages; so does this one.
func TestTheRolesScreenPagesLikeEveryOtherList(t *testing.T) {
	store := seeded()
	for i := range rolesPerPage * 2 {
		store.roles = append(store.roles, &authcontracts.Role{Name: fmt.Sprintf("role_%03d", i)})
	}
	router := mountAs(t, caller{}, withRoles(store))

	code, first, _ := call(t, router, http.MethodGet, "/admin/auth/roles", "")
	if code != http.StatusOK {
		t.Fatalf("the first page = %d %s", code, first)
	}
	if forms := strings.Count(first, `action="/admin/auth/roles"`); forms != rolesPerPage+1 {
		t.Errorf("the first page carries %d forms, want %d roles and the one that creates", forms, rolesPerPage)
	}
	if !strings.Contains(first, `href="/admin/auth/roles?page=2"`) {
		t.Error("the first page does not link the next one")
	}
	// The last page holds the remainder, and the roles are not repeated: a pager
	// that showed the same window every time would be a list with a pager on it.
	code, last, _ := call(t, router, http.MethodGet, "/admin/auth/roles?page=3", "")
	if code != http.StatusOK {
		t.Fatalf("the last page = %d %s", code, last)
	}
	if strings.Contains(last, `id="pk-role-role_000-`) {
		t.Error("the last page shows the first page's roles")
	}
	if !strings.Contains(last, `id="pk-role-role_099-`) {
		t.Error("the last page does not show the last role")
	}
	// A page beyond the end is the last one rather than an empty screen or a
	// fault: a bookmark outlives the rows it was made from.
	if _, beyond, _ := call(t, router, http.MethodGet, "/admin/auth/roles?page=99", ""); !strings.Contains(beyond, "pk-role-role_099-") {
		t.Error("a page past the end is not the last page")
	}
}

// TestTheRolesScreenSurvivesAnUnreachableDatabase keeps the page honest about
// the one failure it cannot render around.
func TestTheRolesScreenSurvivesAnUnreachableDatabase(t *testing.T) {
	broken := &roleStore{roles: seeded().roles, fail: crud.ErrNotFound}
	router := mountAs(t, caller{}, withRoles(broken))
	code, body, _ := call(t, router, http.MethodGet, "/admin/auth/roles", "")
	if code != http.StatusNotFound {
		t.Fatalf("a failed read = %d, want the mapped status: %s", code, body)
	}
	// And it is the shell's fault page rather than the router's, which is what
	// says the screen was reached and the read was what failed.
	if !strings.Contains(body, "Back to the dashboard") {
		t.Errorf("a failed read did not render the shell's fault page: %s", body)
	}
}
