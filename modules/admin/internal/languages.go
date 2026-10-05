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
// this repository yet — T-0181 owns them, and this page takes the heading, Select and
// Checkbox the foundation has over an invented imitation of a stepper.

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

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
			return p.languagesView(ctx, r, nil)
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
				return p.unreadable(ctx, r)
			}
			expected, err := strconv.ParseInt(form.Get("localeRevision"), 10, 64)
			if err != nil {
				return p.unreadable(ctx, r)
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
			// What travels to the view is the refusal's *name* and the revision this
			// form was drawn at, not the command's English sentence: the person reading
			// this page reads it in their own language, and a reason that arrived as
			// `crud: conflict: this declaration was read at revision 2 …` would be a
			// page that declares Portuguese and answers in English over the one line
			// that matters. The command's own error still reaches the log, which is
			// where an English developer sentence belongs.
			return p.languagesView(ctx, r, &refusal{
				status: refused.Status, code: refusalCode(refused.Detail), readAt: expected,
			})
		})
}

// languagesForm is the submitted page: the default, the other languages ticked, and
// the revision this tenant's declaration was read at. The empty set of ticks is a
// value — a tenant served in one language alone is a real declaration — which is why
// the command's floor is the default and not the list.
type languagesForm struct {
	RawBody []byte `contentType:"application/x-www-form-urlencoded"`
}

// codeUnreadableForm names the one refusal this page makes for itself, rather than
// reading it off a command: the body it was handed is not a form, or carries no
// revision to write at. Like every other name in `refusal.code` it is the part of a
// refusal a catalogue can be looked up under, and so the part that is not English.
const codeUnreadableForm = "FORM_UNREADABLE"

// unreadable is the page's own answer to a body it could not read: the same verdict
// ui/page's form decoder gives — 422 — and the page it was submitted from, with the
// sentence in the language of the person holding it.
//
// It is not the kernel's refusal page. ui/page's fault table translates a guard's
// own verdicts and leaves a 422 in the writer's words on purpose, because a 422
// normally says something about the caller's request that only its writer can say
// (see ui/page's faultKey). Here the writer is this page, so the page says it: a
// person who filled the form in Portuguese and had it refused is refused in
// Portuguese, and the document declares the language it is actually printed in.
func (p pages) unreadable(ctx context.Context, r page.Request) (page.View, error) {
	v, err := p.languagesView(ctx, r, &refusal{
		status: http.StatusUnprocessableEntity, code: codeUnreadableForm,
	})
	if err != nil {
		return page.View{}, err
	}
	v.Status = http.StatusUnprocessableEntity
	return v, nil
}

// refusal is a declaration this page would not write, told in the facts the page
// holds rather than in the words the command refused with.
type refusal struct {
	// status is what the command answered: 409 means somebody else's declaration
	// went in first, any other 4xx is a pair the command would not accept at all.
	status int
	// code is the refusal's stable name — LANGUAGE_UNUSABLE, LANGUAGE_TAG,
	// AUTH_DENIED — which is the part of a problem document that is not English.
	code string
	// readAt is the revision this very form was drawn at, which is the half of a
	// stale refusal the person can act on and the half no re-read can recover.
	readAt int64
}

// line is the refusal in the language the page is written in. The two revisions a
// stale refusal names are the one this form brought and the one the row holds now,
// both of which this request knows; the sentence that puts them in an order a
// Portuguese reader reads is the catalogue's.
func (n refusal) line(text func(key, fallback string, args ...any) string, heldAt int64) string {
	switch {
	case n.status == http.StatusConflict:
		return text("stale", "This declaration was read at revision %d, and the tenant is on revision %d.",
			n.readAt, heldAt)
	case n.code == codeUnreadableForm:
		return text("refused_form",
			"This form could not be read — its values are missing or malformed. Nothing was changed.")
	case n.code == tenantcontracts.CodeLanguageUnusable:
		return text("refused_unusable",
			"No copy is shipped for that language, so this tenant cannot be served in it. Nothing was changed.")
	case n.code == tenantcontracts.CodeLanguageTag:
		return text("refused_tag", "That is not a language tag. Nothing was changed.")
	case n.code != "":
		return text("refused", "That declaration was refused (%s). Nothing was changed.", n.code)
	}
	return text("refused_unknown", "That declaration was refused. Nothing was changed.")
}

// refusalCode is the name a refusal carries. A problem's detail is `CODE: prose` for
// a refusal kit/httpx or this module made, and `crud: invalid: CODE: prose` once
// kit/rest has classified it; the name is the only part of either that a translator
// cannot re-word.
func refusalCode(detail string) string {
	for _, prefix := range []string{"crud: invalid: ", "crud: conflict: ", "crud: not found: "} {
		detail = strings.TrimPrefix(detail, prefix)
	}
	code, _, _ := strings.Cut(detail, ":")
	for _, r := range code {
		// A name is upper-case letters and underscores; anything else means this
		// detail is prose from beginning to end and there is nothing to look up.
		if r != '_' && !(r >= 'A' && r <= 'Z') {
			return ""
		}
	}
	return code
}

// languagesView is the page: the installation's languages on offer, this tenant's
// declaration marked, and the warning above the save.
func (p pages) languagesView(ctx context.Context, r page.Request, notice *refusal) (page.View, error) {
	text := func(key, fallback string, args ...any) string {
		if r.Locale == nil {
			return fmt.Sprintf(fallback, args...)
		}
		return r.Locale.Text("admin.languages."+key, fallback, args...)
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
	// One content column, measured rather than asserted:
	// e2e/tenant-languages-design.spec.ts reads the left edge of every heading and
	// paragraph at 390 and at 1440 and refuses more than two columns — this page's
	// copy at one edge and the shell header's own account block at the other. That
	// is why nothing on this page is boxed. An Alert sits behind a border, four
	// steps of padding and a 36px icon disc with a gap beside it, which put its
	// sentence 68px in from the page edge and made a third column out of one
	// sentence; a Section behind a Container's padding made a fourth out of the
	// group heading. Both were indistinguishable from the drift the floor exists to
	// catch: copy that agrees with nothing around it.
	body := []g.Node{
		components.Toolbar(components.ToolbarProps{
			Title: title, Subtitle: text("available", "Languages this installation can serve.")}),
	}
	// The refusal and the receipt are a line of copy under the title in the tone the
	// state deserves, carrying the live region that makes a screen reader say it
	// without a reload — `role="alert"` for a write that was refused and wrote
	// nothing, `role="status"` for one that went in. They land where the page's own
	// subtitle lands, so reading the page top-down gives title, what happened,
	// what is on offer.
	switch {
	case notice != nil:
		body = append(body, components.Text(components.TextProps{
			ComponentProps: components.ComponentProps{Attrs: map[string]string{"role": "alert"}},
			Content:        notice.line(text, current.Revision),
			Color:          "danger",
			Element:        "p",
			Measure:        true,
		}))
	case justSaved:
		body = append(body, components.Text(components.TextProps{
			ComponentProps: components.ComponentProps{Attrs: map[string]string{"role": "status"}},
			Content:        text("saved", "Languages saved."),
			Color:          "success",
			Element:        "p",
			Measure:        true,
		}))
	}
	fields := []g.Node{
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
		// The review step, as the heading over the ticks it warns about. It is above
		// the save rather than behind it because what it warns about is the write, not
		// a step of a wizard: removing a language can hide the translations content
		// already holds in it. The content module keeps no inventory of what is
		// translated into what, so this is the truthful generic warning and not a
		// count — and a count invented here would be a number this repository cannot
		// check. SectionHeader is the heading-and-copy block the foundation already
		// composes page introductions out of, and it carries no box of its own.
		//
		// The design system has no <fieldset>, so the group is named by the heading
		// printed over it rather than by a legend: every tick is still labelled by
		// the language tag it is for, so nothing here is an unnamed control.
		components.SectionHeader(components.SectionHeaderProps{
			Level:       2,
			Title:       text("other", "Other languages"),
			Description: text("remove_warning", "Removing a language can hide its content translations. Review them before saving."),
			Measure:     true,
		}),
	}
	fields = append(fields, others...)
	fields = append(fields, components.FormActions(components.FormActionsProps{},
		components.Button(components.ButtonProps{
			Label: text("save", "Save languages"), Type: "submit"})))
	body = append(body, components.Form(components.FormProps{Action: p.at.languages.at, Label: title}, fields...))
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
