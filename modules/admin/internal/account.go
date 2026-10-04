package internal

// account.go is the shell's account surface: the control a person uses to say
// which language their pages are written in, and the one page route that submits
// it.
//
// It lives in the chrome rather than on a resource's screen because the choice is
// about the reader and about no row on any list: kit/rest records the command as
// Self, ui/screens leaves such a command off the generated screens for the reason
// rest.CommandOptions.Self names, and the shell is the one surface every signed-in
// page carries. A person with no way to say it in the browser has only the JSON
// route, which is a door for programs and not for the person the language is for.

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	g "maragu.dev/gomponents"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/page"
)

// AccountLanguage is what the shell asks of the module that owns people. It is a
// port and not an import: the user module decides what a language choice means,
// publishes the event that records it, and refuses a tag this tenant does not
// serve. This module reads the answer and passes a form's through.
//
// Choose takes no user id, because the command it reaches reads who is asking from
// the transaction — that is the half that keeps an account setting from becoming a
// way to edit a colleague — and an id handed to the shell would be an id the
// browser wrote.
//
// Preferred answers the stored choice, empty when nobody chose. This port carries no
// expected revision: `Choose` is the intent it reads as written, and two of those
// that arrive together leave the row at whichever landed last, each told what it
// now holds by the row it gets back. The counted door is AccountLanguageVersion —
// the one a form drawn from a read has to reach, and see it for why a second writer
// of this field is ordinary rather than hypothetical. The counter itself and what
// made it worth adding are modules/user/migrations/000032_user_language_revision.up.sql.
type AccountLanguage interface {
	Preferred(ctx context.Context, tx db.Tx[db.Tenant], userID uuid.UUID) (locale string, err error)
	Choose(ctx context.Context, tx db.Tx[db.Tenant], tag string) error
}

// AccountLanguageVersion is the account door for a composition whose choice is
// counted: the same read, plus the number the read was taken at, and the same write
// with that number brought back.
//
// It is a second port rather than more methods on AccountLanguage because the two
// doors differ in what a lost write costs. `Choose` is an intent — "answer me in
// Portuguese" — and the loser of two simultaneous intents is told which language it
// now holds by the row it gets back. The control in the header is not an intent: it
// is a picture of what the person's row said when the page was drawn, and a form
// posted from an old picture writes an old choice over a new one. That is the
// difference between a write that changes nothing and a write that quietly takes
// somebody's later choice away, and only the revision decides which happened.
//
// A composition that wires no version here keeps the door it always had: the form
// is drawn without a revision and `Choose` answers an intent, which is what that
// port promises and what its documentation says. What it does not get is a form that
// names a revision — the route refuses one rather than guessing which revision the
// submission meant to bring, so the weaker door is reached only by a caller who
// wrote the POST by hand and is told, in their own language, what is missing.
type AccountLanguageVersion interface {
	// Revision answers what revision this person's choice is held at, to draw into
	// the form and bring back with the next choice.
	Revision(ctx context.Context, tx db.Tx[db.Tenant]) (int64, error)
	// ChooseAt is Choose at the revision the form was drawn at. A revision the row
	// has left behind is a conflict that writes nothing and publishes nothing.
	ChooseAt(ctx context.Context, tx db.Tx[db.Tenant], tag string, expectedRevision int64) error
}

// languageRel is the shell's own path for the account form; the composition of the
// address is Mount's, as it is for every page here.
const languageRel = "/language"

// languageControl is the header's select, its expected revision and its one save
// control — or nothing, when this composition wires no account language port, no
// caller is signed in, or the row could not be read.
//
// The languages on offer are the tenant's own declaration: the set the operator
// said this customer is served in, carried on the request that resolved it, so
// nothing here asks a second tenant or holds a first tenant's answer. A person who
// chose a language the tenant no longer serves is answered by the tenant's
// negotiation instead (the user module's rule), and the control shows what is on
// offer now rather than a choice nobody sells.
//
// A read that fails leaves the control off the page rather than showing one whose
// save could only answer with a conflict it cannot name: the frame builds a node
// and cannot fail the request from here, and a door that cannot open is worse than
// a door that is not drawn. The failure is logged, because a person who cannot find
// their own language setting should be findable in the log by whoever they tell.
func languageControl(ctx context.Context, deps frameDeps, r page.Request) g.Node {
	if deps.Language == nil || !r.SignedIn || r.Principal.UserID == uuid.Nil {
		return nil
	}
	tx, live := httpx.TxFrom(ctx)
	if !live {
		return nil
	}
	languages := offeredLanguages(r.Tenant)
	if len(languages) == 0 {
		return nil
	}
	current, err := deps.Language.Preferred(ctx, tx, r.Principal.UserID)
	if err != nil {
		slog.Default().Warn("admin: the account language could not be read, so the control is not offered",
			"principal", r.Principal.UserID.String(), "error", err)
		return nil
	}
	// The revision the choice was drawn at, as a hidden field. It is what makes the
	// submitted form a change from what the person saw rather than a change to
	// whatever the row happens to hold by the time it lands — see
	// AccountLanguageVersion. A door that answers no revision draws no save.
	revision := ""
	if deps.LanguageVersion != nil {
		at, err := deps.LanguageVersion.Revision(ctx, tx)
		if err != nil {
			slog.Default().Warn("admin: the revision behind the account language could not be read, so the control is not offered",
				"principal", r.Principal.UserID.String(), "error", err)
			return nil
		}
		revision = strconv.FormatInt(at, 10)
	}
	// The empty option is the choice to stop choosing: the browser and the tenant's
	// default answer again. It carries its label from the catalogue on every page,
	// whether or not a choice is held — a control whose way back is worded by
	// components.Select's own fallback would say "Choose…" to somebody reading
	// Portuguese, and the option that undoes a choice has to be readable by the
	// person who wants to undo it. It is selectable now that an empty selection is
	// an ordinary value for this field rather than a prompt.
	placeholder := chrome(r, "account.unset", "Use tenant default")
	options := make([]components.SelectOption, 0, len(languages))
	for _, tag := range languages {
		options = append(options, components.SelectOption{Label: tag, Value: tag})
	}
	saved := false
	if req, ok := httpx.RequestFrom(ctx); ok {
		saved = req.URL.Query().Get("language") == "saved"
	}
	controls := []g.Node{
		components.Select(components.SelectProps{
			Name:        "locale",
			Label:       chrome(r, "account.language", "Language"),
			Value:       current,
			Placeholder: placeholder,
			Options:     options,
		}),
		components.Button(components.ButtonProps{
			Label: chrome(r, "account.save", "Save language"), Type: "submit", Variant: "secondary", Size: "sm",
		}),
	}
	if revision != "" {
		controls = append([]g.Node{components.Input(components.InputProps{
			Name: "expectedRevision", Type: "hidden", Value: revision,
		})}, controls...)
	}
	if saved {
		controls = append([]g.Node{components.Text(components.TextProps{
			ComponentProps: components.ComponentProps{Attrs: map[string]string{
				"role": "status", "data-account-language-status": ""}},
			Content: chrome(r, "account.saved", "Language saved."), Size: "xs", Color: "muted",
		})}, controls...)
	}
	return components.Form(components.FormProps{
		ComponentProps: components.ComponentProps{Attrs: map[string]string{"data-account-language": ""}},
		Action:         deps.LanguagePath, Label: chrome(r, "account.language", "Language"),
	}, controls...)
}

// offeredLanguages is the language list this tenant declared, its default first.
// It reads the request's own tenant and asks nothing of any module: the order is
// the only position that means anything, and it is the declaration's own.
func offeredLanguages(t tenancy.Tenant) []string {
	if t.Languages == nil {
		return nil
	}
	out := make([]string, 0, 1+len(t.Languages.Others))
	if t.Languages.Default != "" {
		out = append(out, t.Languages.Default)
	}
	for _, other := range t.Languages.Others {
		if other != t.Languages.Default {
			out = append(out, other)
		}
	}
	return out
}

// accountForm is the submitted language control. It arrives as a form field rather
// than a JSON body because the control is a form — and `locale` is the empty string
// when the person chose "use the tenant default", which is a value and not a missing
// field, so the route reads the field's presence and not its emptiness.
type accountForm struct {
	RawBody []byte `contentType:"application/x-www-form-urlencoded"`
}

// mountAccountLanguage is the page route the control posts to.
//
// It is a page route and not a fetch of the JSON command because what a person
// asked for is a page in another language: the answer is a redirect back to where
// they were standing, which the negotiation then renders in the language they
// chose — with JavaScript switched off, since it is a form and not a swap. The
// command behind it is the one POST /api/v1/user/users/language runs, so the
// rechecks (the principal is the subject, the tenant's set as it is now, the
// expected revision) happen once, in the service, and this route adds none of its
// own. A refusal is the shell's fault page in the person's own language; the
// stored choice is unchanged there, because the command writes nothing it refuses.
//
// The form carries the revision its select box was drawn at, and with a versioned
// door wired that revision is what the command compares — the control was drawn
// from a read, so the submission is a change from something and not a bare intent.
// A submission that names none to a door that counts them is refused here, writing
// nothing: the only form that ever arrives with one is this one, so a POST without
// it did not come from the control, and guessing the revision it should have brought
// would be this route deciding that a stale page was current.
func (p pages) mountAccountLanguage(app *httpx.Router) {
	if p.Language == nil {
		return
	}
	page.Serve(app, p.shell, page.Route{ID: "admin-account-language", Method: http.MethodPost,
		Path: languageRel, Summary: "Choose the language this person is answered in"},
		httpx.SignedIn(), func(ctx context.Context, r page.Request, in *accountForm) (page.View, error) {
			tx, live := httpx.TxFrom(ctx)
			if !live {
				return page.View{}, unreachable
			}
			form, err := url.ParseQuery(string(in.RawBody))
			if err != nil {
				return page.View{}, problem.New(http.StatusUnprocessableEntity, "this form could not be read")
			}
			if err := p.chooseLanguage(ctx, tx, form); err != nil {
				return accountRefusal(r, p.shell, err)
			}
			return page.View{}, httpx.SeeOther(accountReturn(ctx, p.at.dashboard.at))
		})
}

// chooseLanguage hands the submitted control to the module that owns people, at the
// revision the control was drawn at when the composition's account door counts
// them. An expected revision is read as the form wrote it: a field that is absent,
// repeated or not a number is not a revision to compare, it is a form this route
// did not draw, and the answer to that is a refusal that writes nothing.
func (p pages) chooseLanguage(ctx context.Context, tx db.Tx[db.Tenant], form url.Values) error {
	tag := form.Get("locale")
	if p.LanguageVersion == nil {
		if values := form["expectedRevision"]; len(values) != 0 {
			return errAccountFormNoVersion
		}
		return p.Language.Choose(ctx, tx, tag)
	}
	values, ok := form["expectedRevision"]
	if !ok || len(values) != 1 || values[0] == "" {
		return errAccountFormRevision
	}
	at, err := strconv.ParseInt(values[0], 10, 64)
	if err != nil {
		return errAccountFormRevision
	}
	return p.LanguageVersion.ChooseAt(ctx, tx, tag, at)
}

// errAccountFormRevision and errAccountFormNoVersion are the shell's own refusals,
// sentinels rather than problems because accountRefusal is what turns them into a
// page, in the language of the person whose form this was.
var errAccountFormRevision, errAccountFormNoVersion = errors.New("account form revision"), errors.New("account form no version")

// accountRefusal is a refused language choice, in the language the person whose
// choice it was is being answered in.
//
// The module that owns people answers in its own words — "pt is not a language
// this tenant is served in" — and those words name the fault for the log, the
// problem document and whoever is debugging it. A page is not a log line: the
// person standing in front of it chose from a list this installation drew, and the
// sentence that tells them what happened is the shell's own copy, asked at the key
// every other word of this control is asked at. English copy under a page that
// declares Portuguese lies in the same shape as the page that was English to
// everybody, so the declared language follows the sentence actually on the page
// (the rule ui/page/fault.go applies to the kernel's refusals, applied here to the
// shell's).
//
// A fault this function has no sentence for is handed over as it arrived: an
// outage, a permission, a 500 with no detail to print. Inventing a translated
// sentence for a fault whose kind is unknown would put the shell's words over
// whatever the service actually said, which is the one thing a refusal must not do.
func accountRefusal(r page.Request, s page.Shell, err error) (page.View, error) {
	key, english, code, status := accountRefusalCopy(err)
	if key == "" {
		return page.View{}, rest.Fault(err)
	}
	line := chrome(r, key, english)
	v := page.Fault(status, code+": "+line, s.Back, s.BackLabel)
	if line != english && r.Locale != nil {
		v.Language = r.Locale.Language
	}
	return v, nil
}

// accountRefusalCopy is which sentence of the shell's own answers which fault, and
// the status the fault carries. The empty key is the answer for a fault this control
// has no sentence for — see accountRefusal.
func accountRefusalCopy(err error) (key, english, code string, status int) {
	switch {
	case errors.Is(err, errAccountFormRevision):
		return "account.language.revision",
			"This form did not bring the revision your language choice was read at. Nothing was changed.",
			"INVALID_INPUT", http.StatusUnprocessableEntity
	case errors.Is(err, errAccountFormNoVersion):
		return "account.language.unversioned",
			"This installation's account door answers no revisions, so a form cannot change the language. Nothing was changed.",
			"INVALID_INPUT", http.StatusUnprocessableEntity
	case errors.Is(err, crud.ErrInvalid):
		return "account.language.unsupported",
			"That is not a language this tenant serves you in. Nothing was changed.",
			"INVALID_INPUT", http.StatusUnprocessableEntity
	case errors.Is(err, crud.ErrConflict):
		return "account.language.stale",
			"This account's language changed since this page was drawn. Nothing was changed.",
			"CONFLICT", http.StatusConflict
	}
	return "", "", "", 0
}

// accountReturn is where the redirect goes: the page the person was reading, so
// the language changes around them and they do not lose their place. Only a path
// this installation could have served — httpx.LocalPath is the kernel's own rule,
// the one that refuses "/\\evil.example" — and the dashboard for anybody who
// arrived at the route with no page behind them.
func accountReturn(ctx context.Context, back string) string {
	to := back
	if req, ok := httpx.RequestFrom(ctx); ok {
		if at, err := url.Parse(req.Referer()); err == nil && httpx.LocalPath(at.Path) {
			to = at.Path
		}
	}
	if strings.Contains(to, "?") {
		return to + "&language=saved"
	}
	return to + "?language=saved"
}
