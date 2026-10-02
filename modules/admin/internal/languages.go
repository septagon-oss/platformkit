package internal

// languages.go is the tenant's own Settings → Languages page.
//
// Which languages a customer is served in was the operator's declaration about
// them, reachable only at the control plane. That was right about the installation's
// catalogues — an operator decides which languages the deployment ships copy for —
// and wrong about the customer, who cannot ask for Portuguese without a ticket. The
// page writes the tenant's own declaration, through the tenant module's own command,
// on the tenant's own transaction: the same fact, the same event, the same
// normalisation and the same floor under the last language as the operator's route.
//
// The installation's own set — what copy exists at all — is handed to this module
// rather than guessed: the page offers what the composition can answer in, and the
// command refuses anything else. A select offering a language no catalogue carries
// would be a page that declares a tongue it does not speak.
//
// One screen holds the default and the set, and the review step above the save names
// what removing a language can cost. A two-step disclosure with progress is the
// brief's shape for this, and the drawer and stepper that would carry it are not in
// this repository yet — T-0181 owns them, and this page takes the Section and Select
// that exist over an invented imitation of a stepper.

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/rest"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/page"
)

// mountLanguages is the page and its one write. Both are guarded by the tenant's own
// permission rather than the operator's: whoever may open this screen may already
// change the tenant's languages through the command, and the screen is what lets them
// do it without a JSON client.
func (p pages) mountLanguages(app *httpx.Router) {
	guard := httpx.Permission(tenantcontracts.PermissionLocaleManage)
	page.Serve(namespace(app, "tenant"), p.shell, page.Route{ID: "admin-languages", Method: http.MethodGet,
		Path: p.at.languages.rel, Summary: "Which languages this tenant is served in"}, guard,
		func(ctx context.Context, r page.Request, _ *page.Empty) (page.View, error) {
			return p.languagesView(ctx, r, "")
		})

	page.Serve(namespace(app, "tenant"), p.shell, page.Route{ID: "admin-languages-set", Method: http.MethodPost,
		Path: p.at.languages.rel, Summary: "Set which languages this tenant is served in"}, guard,
		func(ctx context.Context, r page.Request, in *languagesForm) (page.View, error) {
			tx, live := httpx.TxFrom(ctx)
			if !live {
				return page.View{}, unreachable
			}
			form, err := url.ParseQuery(string(in.RawBody))
			if err != nil {
				return page.View{}, unreadableForm
			}
			expected, err := strconv.ParseInt(form.Get("localeRevision"), 10, 64)
			if err != nil {
				return page.View{}, unreadableForm
			}
			_, err = p.Tenants.SetOwnLocale(ctx, tx, expected, tenantcontracts.SetLocale{
				Default: form.Get("default"), Supported: form["supported"],
			})
			if err == nil {
				return page.View{}, httpx.SeeOther(p.at.languages.at + "?languages=saved")
			}
			// A refusal comes back to the page it was made from, with the sentence a
			// person can act on above the form — the same choice the roles screen makes
			// and for the same reason: the fault page would throw away the set somebody
			// was halfway through choosing. A refusal wrote nothing, so what they read
			// back is the declaration as it stands.
			refused, named := rest.Fault(err).(*problem.Problem)
			if !named || refused.Status >= http.StatusInternalServerError {
				return page.View{}, err
			}
			_, detail := rest.FieldErrors(refused, nil)
			return p.languagesView(ctx, r, detail)
		})
}

// languagesForm is the submitted page: the default, the other languages ticked, and
// the revision this tenant's declaration was read at. The empty set of ticks is a
// value — a tenant served in one language alone is a real declaration — which is why
// the command's floor is the default and not the list.
type languagesForm struct {
	RawBody []byte `contentType:"application/x-www-form-urlencoded"`
}

// unreadableForm is what a body that is not a form answers with — the same answer
// ui/resource's generated forms give, so one 422 means one thing in this shell.
var unreadableForm = problem.New(http.StatusUnprocessableEntity, "this form could not be read")

// languagesView is the page: the installation's languages on offer, this tenant's
// declaration marked, and the warning above the save.
func (p pages) languagesView(ctx context.Context, r page.Request, detail string) (page.View, error) {
	text := func(key, fallback string) string {
		if r.Locale == nil {
			return fallback
		}
		return r.Locale.Text("admin.languages."+key, fallback)
	}
	tx, live := httpx.TxFrom(ctx)
	if !live {
		return page.View{}, unreachable
	}
	current, err := p.Tenants.OwnLocale(ctx, tx)
	if err != nil {
		return page.View{}, rest.Fault(err)
	}
	justSaved := false
	if req, ok := httpx.RequestFrom(ctx); ok {
		justSaved = req.URL.Query().Get("languages") == "saved"
	}
	options := make([]components.SelectOption, 0, len(p.Installed))
	others := make([]g.Node, 0, len(p.Installed))
	for _, tag := range p.Installed {
		options = append(options, components.SelectOption{Label: tag, Value: tag})
		others = append(others, components.Checkbox(components.CheckboxProps{
			Name:    "supported",
			Label:   tag,
			Value:   tag,
			Checked: tag != current.Default && contains(current.Supported, tag),
		}))
	}
	title := text("title", "Languages")
	body := []g.Node{
		components.Toolbar(components.ToolbarProps{
			Title: title, Subtitle: text("available", "Languages this installation can serve.")}),
	}
	if detail != "" {
		body = append(body, components.Alert(components.AlertProps{Tone: "danger", Message: detail, Bordered: true}))
	}
	body = append(body,
		components.Form(components.FormProps{Action: p.at.languages.at, Label: title},
			components.Select(components.SelectProps{
				Name:     "default",
				Label:    text("default", "Default language"),
				Value:    current.Default,
				Options:  options,
				Required: true,
			}),
			components.Input(components.InputProps{
				Name:  "localeRevision",
				Type:  "hidden",
				Value: strconv.FormatInt(current.Revision, 10),
			}),
			components.Section(components.SectionProps{Gap: "4"}, components.SectionSlots{
				Header: []g.Node{components.SectionHeader(components.SectionHeaderProps{
					Title: text("other", "Other languages")})},
				Body: others,
			}),
			// The review step. It is above the save rather than behind it because what
			// it warns about is the write, not a step of a wizard: removing a language
			// can hide the translations content already holds in it. The content module
			// keeps no inventory of what is translated into what, so this is the truthful
			// generic warning and not a count — and a count invented here would be a
			// number this repository cannot check.
			components.Alert(components.AlertProps{
				Tone: "warning", Bordered: true,
				Message: text("remove_warning", "Removing a language can hide its content translations. Review them before saving."),
			}),
			components.FormActions(components.FormActionsProps{},
				components.Button(components.ButtonProps{
					Label: text("save", "Save languages"), Type: "submit"})),
		))
	if justSaved {
		body = append([]g.Node{components.Alert(components.AlertProps{
			Tone: "success", Bordered: true, Message: text("saved", "Languages saved."),
		})}, body...)
	}
	return page.View{Title: title, Body: body}, nil
}

func contains(set []string, want string) bool {
	for _, have := range set {
		if have == want {
			return true
		}
	}
	return false
}
