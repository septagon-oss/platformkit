package internal

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/page"
	"github.com/septagon-oss/platformkit/ui/resource"
)

// Roles is what the shell needs of the auth module to serve the screen that
// module's nav entry names. It is narrower than authcontracts.Service for the
// reason auth.Deps.Users is narrower than the user module's own: a consumer
// depends on the capability it uses, which is also what makes a test's stand-in
// four lines.
type Roles interface {
	// Roles is every role in this tenant, in name order.
	Roles(ctx context.Context, tx db.Tx[db.Tenant]) ([]*authcontracts.Role, error)
	// SetRole writes what one role grants, creating it if it is new. declared is
	// the kernel's catalogue, which the shell reads once at mount.
	SetRole(ctx context.Context, tx db.Tx[db.Tenant], name string, permissions []string,
		declared []tenancy.Grant) (*authcontracts.Role, error)
}

// rolesPerPage is a screenful, and it is ui/resource's number so that this
// hand-written list pages the way every generated one does.
//
// A row here is a form with one control per declared permission rather than a
// table row, so a page of them is heavier than a page of a list. Bounded is the
// point. Without a page this screen turned a tenant's roles table into as much
// HTML as that table was long — measured at fifty megabytes for two thousand
// roles, against a quarter of a megabyte from the JSON route over the same rows
// — and kit/httpx holds a response in memory until the transaction commits, in
// a process every tenant of the installation shares. Roles are created through
// this same screen, one POST each.
const rolesPerPage = resource.PerPage

// unreachable is the answer when the request has no transaction or no
// connection: the middleware has already logged what happened, and a page that
// said more would be guessing.
var unreachable = problem.New(http.StatusServiceUnavailable, "the database is not reachable right now")

// The statuses these two answer with. Both are spelled out because httpx.HTML
// substitutes its own list only for a page that declares none: a page that
// names one status replaces the set rather than adding to it, so the write —
// which does have a refusal of its own to declare — silently stopped declaring
// the 404 and the 503 it can still answer.
var (
	rolesReadFaults  = []int{http.StatusNotFound, http.StatusServiceUnavailable}
	rolesWriteFaults = []int{http.StatusNotFound, http.StatusUnprocessableEntity, http.StatusServiceUnavailable}
)

// rolesInput is which screenful to show.
type rolesInput struct {
	Page int `query:"page" minimum:"1" default:"1"`
}

// roleForm is the submitted form: a role's name, the permissions that were
// ticked, and the page it was on. Absent permissions mean a role that grants
// nothing, which is a role somebody may deliberately want — member is one — so
// an empty list is a value and not a missing field.
type roleForm struct {
	RawBody []byte `contentType:"application/x-www-form-urlencoded"`
}

// mountRoles is the sixth hand-written page, and it is here rather than in
// modules/auth for the reason every other page here is: this module is composed
// last and owns the shell, so it is the only one that can put a screen inside
// the admin chrome. It is here rather than generated for a different reason —
// a role is keyed by its name rather than by an id, and kit/rest's screens are
// mounted from a Spec whose item path is a UUID.
//
// A composition that wires no Roles capability mounts nothing, and then the
// auth module's nav entry is unserved and Mount says so at boot, which is the
// truth: there is no screen.
func (p pages) mountRoles(api *httpx.API) {
	if p.Roles == nil {
		return
	}
	// The same permission the two JSON routes declare. There is no role:read
	// beside it, so whoever may open this screen may already change a role
	// through the API; the screen is what makes that possible without curl.
	guard := httpx.Permission(authcontracts.PermissionRoleManage)

	page.Serve(api, p.shell, page.Route{ID: "admin-roles", Method: http.MethodGet, Path: rolesPath,
		Summary: "What each role grants in this tenant", Errors: rolesReadFaults}, guard,
		func(ctx context.Context, r page.Request, in *rolesInput) (page.View, error) {
			return p.rolesView(ctx, r, in.Page, "")
		})

	page.Serve(api, p.shell, page.Route{ID: "admin-role-set", Method: http.MethodPost, Path: rolesPath,
		Summary: "Set what a role grants", Errors: rolesWriteFaults}, guard,
		func(ctx context.Context, r page.Request, in *roleForm) (page.View, error) {
			tx, live := httpx.TxFrom(ctx)
			if !live {
				return page.View{}, unreachable
			}
			form, err := url.ParseQuery(string(in.RawBody))
			if err != nil {
				return page.View{}, problem.New(http.StatusUnprocessableEntity, "this form could not be read")
			}
			// The screenful the form was on, so that a refusal and a success
			// both come back to what the person was reading.
			at, _ := strconv.Atoi(form.Get("page"))
			if _, err = p.Roles.SetRole(ctx, tx, form.Get("name"), form["permissions"], p.declared); err == nil {
				return page.View{}, httpx.SeeOther(pageURL(at))
			}
			// A refusal names the role or the permission that caused it. The
			// page comes back with that message on it rather than as the
			// shell's fault page, which would lose every other role's state
			// and say "Back to the dashboard" to somebody who is two ticks
			// from getting it right.
			refused, named := rest.Fault(err).(*problem.Problem)
			if !named || refused.Status >= http.StatusInternalServerError {
				return page.View{}, err
			}
			// Through kit/rest's own reader, so this screen says what a
			// generated form says: the message a person can act on, without the
			// package prefix kit/crud puts in front of it.
			_, detail := rest.FieldErrors(refused, nil)
			return p.rolesView(ctx, r, at, detail)
		})
}

// rolesView reads this tenant's roles and renders one screenful. detail is a
// refusal to show above them, empty on the way in.
func (p pages) rolesView(ctx context.Context, r page.Request, at int, detail string) (page.View, error) {
	tx, live := httpx.TxFrom(ctx)
	if !live {
		return page.View{}, unreachable
	}
	roles, err := p.Roles.Roles(ctx, tx)
	if err != nil {
		return page.View{}, rest.Fault(err)
	}
	return rolesPage(roles, p.declared, r.Tenant.Operator, at, detail), nil
}

// rolesPage is the screen as a function of values: the roles there are, the
// permissions this tenant may name, which screenful to show, and a refusal.
//
// One form per role, each posting the whole list back. That is the shape of the
// route underneath — a PUT replaces what a role grants — so a form that posted
// a change instead would be describing a write the server does not have.
func rolesPage(roles []*authcontracts.Role, declared []tenancy.Grant, operator bool, at int, detail string) page.View {
	offered := offeredPermissions(declared, operator)
	total := len(roles)
	last := max((total+rolesPerPage-1)/rolesPerPage, 1)
	// A page past the end is the last one rather than an empty screen: a
	// bookmark outlives the rows it was made from.
	at = min(max(at, 1), last)
	from := (at - 1) * rolesPerPage
	shown := roles[from:min(from+rolesPerPage, total)]

	status := 0
	body := []g.Node{components.Toolbar(components.ToolbarProps{
		Title:    "Roles",
		Subtitle: counted(total) + " What each role name grants here, which is what everybody holding one may do.",
	})}
	if detail != "" {
		status = http.StatusUnprocessableEntity
		body = append(body, components.Alert(components.AlertProps{
			Tone: "danger", Message: detail, Bordered: true}))
	}
	for _, role := range shown {
		body = append(body, roleFieldset(role.Name, role.Grants, offered, at))
	}
	body = append(body,
		components.Pagination(components.PaginationProps{
			CurrentPage: at, TotalPages: last, BaseURL: rolesPath,
			NavigationLabel: "Roles pagination",
		}),
		components.Divider(components.DividerProps{Text: "New role"}),
		newRoleFieldset(offered, at))
	return page.View{Title: "Roles", Status: status, Body: body}
}

// counted is how many there are, the way a generated list says it before saying
// what they are.
func counted(total int) string {
	if total == 1 {
		return "1 role."
	}
	return strconv.Itoa(total) + " roles."
}

// pageURL is where a write returns to: the screenful it was made on.
func pageURL(at int) string {
	if at <= 1 {
		return rolesPath
	}
	return rolesPath + "?page=" + strconv.Itoa(at)
}

// offeredPermissions is every permission this tenant may name, the wildcard
// first because it is the rule rather than a permission.
//
// An operator permission is offered only in the operator's own tenant.
// contracts.CheckedPermissions refuses one anywhere else and the kernel would
// refuse every request under it anyway, so a checkbox for one in a customer's
// tenant is a control whose only outcome is a 422.
func offeredPermissions(declared []tenancy.Grant, operator bool) []string {
	named := make([]string, 0, len(declared))
	for _, grant := range declared {
		if grant.Operator && !operator {
			continue
		}
		named = append(named, grant.Permission)
	}
	slices.Sort(named)
	return append([]string{authcontracts.Wildcard}, named...)
}

// roleFieldset is one role: its name, what it grants, and the one button that
// writes the two together.
func roleFieldset(name string, granted []string, offered []string, at int) g.Node {
	form := "pk-role-" + name
	nodes := []g.Node{
		components.Heading(components.HeadingProps{Text: name, Level: 2}),
		hidden(form+"-name", "name", name),
		hidden(form+"-page", "page", strconv.Itoa(at)),
	}
	// What this role holds that this screen has no control for: a permission no
	// module declares any more, or one that belongs to the installation rather
	// than to this tenant. The form cannot render it and the write replaces the
	// whole list, so saving drops it — which is a legal thing to want and a
	// terrible thing to discover afterwards. So the form says it before the
	// button does it. internal.Undeclared is the hourly sweep's half of the
	// same fact, and it exists because this state is supported rather than
	// broken.
	if lost := notOffered(granted, offered); len(lost) > 0 {
		nodes = append(nodes, components.Alert(components.AlertProps{
			Tone: "warning", Compact: true, Bordered: true,
			Message: fmt.Sprintf("Saving %s also drops %s, which this application no longer offers here.",
				name, strings.Join(lost, ", ")),
		}))
	}
	nodes = append(nodes,
		permissionBoxes(form, granted, offered),
		components.FormActions(components.FormActionsProps{},
			components.Button(components.ButtonProps{Label: "Save " + name, Type: "submit"})))
	return components.Form(components.FormProps{Action: rolesPath, Label: "What " + name + " grants"}, nodes...)
}

// newRoleFieldset is the same form with the name left to be typed. There is no
// separate create route: SetRole writes the role it is given whether or not it
// existed, so a second form would be a second door to one write.
func newRoleFieldset(offered []string, at int) g.Node {
	// A role name cannot contain a hyphen, so this prefix cannot collide with
	// the form of a role somebody has actually called "new".
	const form = "pk-new-role"
	return components.Form(components.FormProps{Action: rolesPath, Label: "A new role"},
		components.Input(components.InputProps{
			ComponentProps: components.ComponentProps{ID: form + "-name"},
			Name:           "name", Label: "Name", Required: true, MaxLength: authcontracts.MaxRoleName,
			HelpText: "A lower-case identifier: letters, digits and underscores.",
			Pattern:  "[a-z][a-z0-9_]*", Placeholder: "editor"}),
		hidden(form+"-page", "page", strconv.Itoa(at)),
		permissionBoxes(form, nil, offered),
		components.FormActions(components.FormActionsProps{},
			components.Button(components.ButtonProps{Label: "Create the role", Type: "submit"})),
	)
}

// hidden is a value the form carries and nobody edits.
func hidden(id, name, value string) g.Node {
	return components.Input(components.InputProps{
		ComponentProps: components.ComponentProps{ID: id},
		Type:           "hidden", Name: name, Value: value})
}

// notOffered is what a role grants that this screen has no control for, in the
// order the role holds it.
func notOffered(granted, offered []string) []string {
	var lost []string
	for _, permission := range granted {
		if !slices.Contains(offered, permission) {
			lost = append(lost, permission)
		}
	}
	return lost
}

// permissionBoxes is the catalogue as controls. Every box in every form is
// named "permissions", because that is what the handler reads; each carries an
// id built from its own form, because several forms on one page otherwise
// render the same id and a label then points at whichever input the browser
// found first — a person ticking member's boxes would be ticking admin's.
func permissionBoxes(form string, granted []string, offered []string) g.Node {
	boxes := make([]g.Node, 0, len(offered))
	for _, permission := range offered {
		label := permission
		if permission == authcontracts.Wildcard {
			label = "* — everything in this tenant"
		}
		boxes = append(boxes, components.Checkbox(components.CheckboxProps{
			ComponentProps: components.ComponentProps{ID: boxID(form, permission)},
			Name:           "permissions",
			Value:          permission,
			Label:          label,
			Checked:        slices.Contains(granted, permission),
		}))
	}
	return components.Grid(components.GridProps{Columns: "1", SM: "2", LG: "3", Gap: "2"}, boxes...)
}

// boxID is one control's DOM identity: the form it belongs to and the
// permission it names, with the two characters a permission carries that a
// selector would otherwise have to escape spelled out.
func boxID(form, permission string) string {
	if permission == authcontracts.Wildcard {
		permission = "everything"
	}
	return form + "-" + strings.ReplaceAll(permission, ":", "-")
}
