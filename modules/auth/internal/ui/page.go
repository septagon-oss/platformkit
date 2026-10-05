package ui

// page.go is the page the address in the verification mail opens, and the only
// file in this module that writes markup.
//
// Until now the mail carried an address and nothing answered it: the module
// registered the JSON door at /api/v1/public/auth/verify-email and no page, so
// the one thing a new person does with the message — open the link — landed on
// the kernel's 404. The journey was green because it read the credential out of
// the message and posted the door itself, which is what an SDK does and not what
// a person does.
//
// Two routes, one command, the shape ui/page/access.go already sets down for the
// ask form:
//
//   - GET the address renders the state the credential points at and a form. It
//     reads the token and consumes nothing, because a mail scanner fetches every
//     link in every message it is handed, and a link that spent itself on sight
//     would mean the person behind it has to ask for another one. This is the
//     same reason the consumption door is a POST and not the page.
//   - POST the address spends the credential through the module's one command —
//     not a second implementation of it (docs/adr/0007) — and redirects to the
//     sign-in page the composition named, because a confirmed account's next step
//     is signing in with the password it already chose.
//
// The page carries a bearer credential in its body and its form, so it is
// marked Sensitive: no-store, and no referrer leaked to whatever the page links.
//
// What this file may reach is the port below and the foundation's layers, and
// nothing else: the credential, the account state and the command that spends
// them stay in the package that owns them, which imports this one.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	uiroot "github.com/septagon-oss/platformkit/ui"
	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/page"
)

// verifyEmailRel is this page's address relative to the module and its surface:
// mounted on the public face of "auth" it composes to /auth/verify-email, which
// is the address the mail writes (contracts.VerifyEmailPath). The mount below
// refuses the two spellings if they ever disagree, because a mail that names an
// address the composition moved is a broken journey nobody notices until a new
// account cannot be created.
const (
	verifyEmailRel    = "/verify-email"
	confirmEmailLabel = "Confirm email address"
	// brand is what the page calls the installation when the tenant has no name,
	// the same word the shell and the public site use.
	brand = "PlatformKit"
	// sourceLanguage is the language this module's own page is written in, said
	// the way document.View.Language says it. See the note on the View below.
	sourceLanguage = "en"
	// maxTokenLength bounds the credential this page will read, and is the same
	// limit the JSON door puts on its body field.
	maxTokenLength = 128
	// The two sentences this page refuses with, in the language they are authored in, and
	// the catalogue entries that carry them in the others. The sentence stays in the code
	// because it is what a program reads — the problem body an SDK parses says exactly
	// this — and the key is what a person is shown, because ui/page renders a refusal in
	// the language the reader asked for out of the catalogues the composition merged.
	// A module with a page has a person reading its refusals, so it ships their copy the
	// way it ships its permission labels: modules/auth/messages, under its own prefix.
	linkInvalid    = "that verification link is invalid or has expired; request another link"
	linkNeeded     = "this address needs the link from the email; ask for another one"
	keyLinkInvalid = "auth.verify.link_invalid"
	keyLinkNeeded  = "auth.verify.link_needed"
)

// refuse is one of this page's refusals, in both of the shapes one verdict has: the
// sentence a program reads, as it has always been, and the key of the copy this module
// ships for a person. Resolving that key is the shell's — it owns the catalogues, the
// tenant's declared languages and the negotiation — and this module's share is the sentence
// and the entry it is filed under.
func refuse(status int, key, sentence string) error {
	refused := problem.New(status, sentence)
	refused.Key = key
	return refused
}

// Pages is the chrome of this page, and the composition owns every word of it:
// the palette, where the sheet is served, and where a person who has just
// confirmed their address is sent. See auth.Deps.Pages.
type Pages struct {
	// Theme is the installation's two palettes; the zero value gets the shipped
	// one, so that a page is never served a document with no tokens in it.
	Theme design.Pair
	// Assets is where the installation's stylesheet is served, as the
	// composition mounted it: the document this page renders links
	// Assets + "/app.css", so it is the sheet one mount serves, not a second
	// copy of it. apps/platformkit pins the same address for the refusal pages
	// it renders outside the shell.
	Assets string
	// SignIn is the workspace's sign-in page: where a person who has just
	// confirmed their address goes, and the one way back the page offers.
	SignIn string
	// Messages opts the page into the composition's merged catalogues, as the
	// admin shell's and the public site's Deps of the same name do. Nil keeps it
	// in the source language.
	Messages page.Messages
}

// Confirmation is this page's whole dependence on the module that owns it: the
// one command that spends the credential, and the one counter that bounds how
// often one machine may ask. The command is the same one the JSON door calls —
// this page is a second door onto it, not a second implementation — and it
// arrives as a value handed over at composition rather than as an import of the
// package that has it, which is what keeps the markup on this side of the line.
type Confirmation interface {
	// Addressee says whose address a live credential points at, so the page can
	// name it back. It spends nothing.
	Addressee(ctx context.Context, token string) (string, error)
	// Spend consumes the credential and moves the account it names.
	Spend(ctx context.Context, token string) error
	// MayRedeem counts one attempt from one machine's request and says whether
	// it is inside the budget.
	MayRedeem(ctx context.Context, r *http.Request) bool
}

// verifyForm is the submitted form: the credential, written into a hidden field
// by the page that carries it. Nothing else is submittable, because nothing else
// confirming an address may decide — not the roles, not the password, both of
// which the registration already fixed.
type verifyForm struct {
	RawBody []byte `contentType:"application/x-www-form-urlencoded"`
}

type verifyEmailQuery struct {
	// Token carries no `maxLength` tag on purpose. huma validates a request before any
	// handler runs and answers a failure itself, in its problem JSON, which is the right
	// answer for an API door and the wrong one for a page: the person who pasted a link
	// with a stray character on the end was shown JSON in a browser window. The limit is
	// this handler's to apply — the same 128, refused with the same verdict — so that the
	// refusal arrives through the page seam every other refusal of this address arrives
	// through, in the tenant's language, with the reference the person can quote. The JSON
	// door at /api/v1/public/auth/verify-email keeps its own schema and its own 422.
	Token string `query:"token" doc:"The one-time credential the mail wrote into its link"`
}

// Mount serves the link's page on the module's public face. cmd is the module's
// own command behind the port above; p is the installation's chrome.
func Mount(s httpx.Surfaces, cmd Confirmation, p Pages) {
	public := s.Public
	if p.Theme == (design.Pair{}) {
		p.Theme = design.Default()
	}
	sheet := uiroot.Compose(p.Theme)
	shell := page.Shell{
		Chrome:    page.Chrome{Brand: brand, Assets: p.Assets, Stylesheet: sheet, SignIn: p.SignIn},
		Frame:     bare,
		Tag:       "auth",
		Back:      p.SignIn,
		BackLabel: "Sign in",
		Messages:  p.Messages,
	}
	if at := public.PagePath(verifyEmailRel); at != contracts.VerifyEmailPath {
		panic(fmt.Sprintf("auth: the verification mail writes %s, but this composition serves its page at %s",
			contracts.VerifyEmailPath, at))
	}
	page.Serve(public, shell, page.Route{
		ID: "auth-verify-email-page", Method: http.MethodGet, Path: verifyEmailRel,
		Summary: "The page the emailed verification link opens",
		Errors:  []int{http.StatusUnauthorized, http.StatusUnprocessableEntity, http.StatusServiceUnavailable},
	}, httpx.Public(), func(ctx context.Context, _ page.Request, in *verifyEmailQuery) (page.View, error) {
		if in.Token == "" {
			return page.View{}, refuse(http.StatusUnauthorized, keyLinkNeeded, linkNeeded)
		}
		if len(in.Token) > maxTokenLength {
			// The verdict the schema used to answer with, kept where it belongs: a
			// credential this long is not a link anybody clicked, and 422 is what every
			// other refusal of an unusable request here says. It is refused before the
			// lookup, so nothing reaches the store with a megabyte of query in it.
			return page.View{}, refuse(http.StatusUnprocessableEntity, keyLinkInvalid, linkInvalid)
		}
		email, err := cmd.Addressee(ctx, in.Token)
		if errors.Is(err, contracts.ErrCredentials) {
			return page.View{}, refuse(http.StatusUnauthorized, keyLinkInvalid, linkInvalid)
		}
		if err != nil {
			return page.View{}, err
		}
		// Language is the source language and not the negotiated one, for the
		// reason modules/web/internal declares its own: every line below is a Go
		// string here, and a document that declared Portuguese over English text
		// would be telling a screen reader to read English with a Portuguese
		// voice. What the shell translates for this page is the refusal — an
		// expired or spent link is answered in the reader's language, because that
		// sentence arrives from a catalogue.
		v := page.View{
			Title: "Confirm your email address", Status: http.StatusOK, Sensitive: true,
			Language: sourceLanguage,
			Body: []g.Node{
				components.Toolbar(components.ToolbarProps{Title: "Confirm your email address"}),
				components.Text(components.TextProps{Content: "Confirm that " + email + " is yours to finish creating the account. The password and the roles stay as they were."}),
				components.Form(components.FormProps{Action: public.PagePath(verifyEmailRel), Label: "Email confirmation"},
					h.Input(h.Type("hidden"), h.Name("token"), h.Value(in.Token)),
					components.FormActions(components.FormActionsProps{},
						components.Button(components.ButtonProps{Label: confirmEmailLabel, Type: "submit", Variant: "primary"})),
				),
				components.Link(components.LinkProps{Label: "Sign in", Href: p.SignIn}),
			},
		}
		return v, nil
	})

	page.Serve(public, shell, page.Route{
		ID: "auth-verify-email-confirm", Method: http.MethodPost, Path: verifyEmailRel,
		Summary: "Confirm the email address, from the page the link opened",
		Errors:  []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests, http.StatusUnprocessableEntity},
	}, httpx.Public(), func(ctx context.Context, _ page.Request, in *verifyForm) (page.View, error) {
		r, _ := httpx.RequestFrom(ctx)
		if !httpx.SameSite(r) {
			return page.View{}, problem.New(http.StatusForbidden, "confirm the email from the verification page itself")
		}
		if !cmd.MayRedeem(ctx, r) {
			return page.View{}, problem.New(http.StatusTooManyRequests, "too many account link attempts; wait and try again")
		}
		form, err := url.ParseQuery(string(in.RawBody))
		if err != nil {
			return page.View{}, page.FormUnreadable()
		}
		token := form.Get("token")
		if token == "" || len(token) > maxTokenLength {
			return page.View{}, refuse(http.StatusUnauthorized, keyLinkInvalid, linkInvalid)
		}
		if err := cmd.Spend(ctx, token); err != nil {
			if errors.Is(err, contracts.ErrCredentials) {
				return page.View{}, refuse(http.StatusUnauthorized, keyLinkInvalid, linkInvalid)
			}
			return page.View{}, err
		}
		return page.View{}, httpx.SeeOther(p.SignIn)
	})
}

// bare is the frame: a narrow column, no navigation, because a person arriving
// from an email has one thing to do and no workspace to navigate yet.
func bare(_ context.Context, _ page.Request, body []g.Node) g.Node {
	return page.Bare(body)
}
