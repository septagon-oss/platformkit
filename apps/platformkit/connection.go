package main

// connection.go is the workspace's face, answered as data to somebody who has not
// signed in yet.
//
// Everything the document answers already exists and is already read before
// sign-in — by the browser, as HTML, by modules/web. What did not exist is a
// machine-readable answer at the same host: a name, a mark, an accent, and the
// doors this installation actually mounted. The join belongs here and in no module
// for one reason: its four owners are four different modules, and a module that
// reached across to three others would invert the tier rule every one of them is
// built under — modules/site knows nothing of auth, modules/tenant knows nothing of
// the file module. The composition is the one place that has met all four, and it
// reads only their contracts/.
//
// The address, /api/v1/app/connection, is the kernel's in the same way
// /api/v1/app/resources is: no module composes the module-less /api/v1/app prefix,
// so this mount can only be the composition's, and it goes through the one seam
// that reaches that prefix — app.Options.WorkspaceCatalog, which takes an arbitrary
// mount function (see workspaceFace in fault.go for why the field's name is now
// narrower than what runs through it).

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	sitecontracts "github.com/septagon-oss/platformkit/modules/site/contracts"
	"github.com/septagon-oss/platformkit/ui/screens"
)

// brandName is the installation's own word for itself, and the last name a
// workspace falls back to — the same word modules/web's `brand` falls back to, and
// TestTheInstallationNamesItselfTheSameWayEverywhere asks that the two agree,
// because two fallbacks spelling two names is a product with two names.
const brandName = "PlatformKit"

// connectionFace is the composition's answer about itself. Its one field is filled
// once the sentence has resolved — the same late-bound shape as `ask` and `shell`
// in sentences, for the same reason: the services are read off the plan, and the
// mount is written before the plan exists.
type connectionFace struct {
	describe func(ctx context.Context) (*screens.Connection, error)
}

// mountConnection registers the document on the workspace surface. The route is
// `Public` for the reason the sign-in door is: what it answers is what an
// anonymous visitor of this host may already see, and it names no permission
// because there is none to name. httpx records that declaration in the published
// document, and kit/app prints every public door at boot, so the opening is
// auditable rather than quiet.
func (f *connectionFace) mount(api *httpx.API) {
	httpx.Register(api.Surfaces("").App, huma.Operation{
		OperationID: "app-connection",
		Method:      http.MethodGet,
		Path:        "/connection",
		Summary:     "This workspace's name, mark and accent, and how one signs in here",
		Description: "What a shell may show before anybody has signed in: nothing about a person, a count or a plan. " +
			"The workspace comes from the request's own host and from nothing else, so there is no parameter to enumerate. " +
			"A host that serves no workspace is refused rather than answered with an empty body, which would be an oracle for which hosts exist.",
		Tags:   []string{"kernel"},
		Errors: []int{http.StatusNotFound, http.StatusServiceUnavailable},
	}, httpx.Public(), func(ctx context.Context, _ *struct{}) (*connectionDocument, error) {
		if _, ok := tenancy.FromContext(ctx); !ok {
			// A Public door is reached at hosts that resolve to no tenant — an
			// unknown host, and a loader that cannot tell the two apart. Both get
			// "nothing is served here" with no body: the same refusal
			// kit/rest/singleton.go and modules/web/internal/mount.go make, and the
			// acceptance case that a 200 with an empty body would break.
			return nil, problem.NotFound("no workspace is served at this host")
		}
		if f.describe == nil {
			// Not a caller's problem and not a 500 either: a build that mounted the
			// door and never filled it is this composition's own defect, and the
			// phone is told nothing is available rather than handed a panic.
			return nil, problem.New(http.StatusServiceUnavailable, "this installation serves no workspace description")
		}
		body, err := f.describe(ctx)
		if err != nil {
			return nil, err
		}
		return &connectionDocument{Body: body}, nil
	})
}

// connectionDocument is the envelope, and it exists for the reason kit/app's
// workspaceDocument does: a body handed to huma as `any` leaves its fields out of
// the published document, and this document is what a shell is written against.
// The type has to survive to the schema.
type connectionDocument struct {
	Body *screens.Connection
}

// describe is the read, in the request's own tenant transaction — the one the host
// resolution opened, so row-level security scopes every query with no help from
// here, and a wrong-tenant read is not something this handler could arrange even
// by naming another tenant: there is no parameter to name them in.
//
// Every read is the unlocked one. This route writes nothing, so it takes no row
// lock; an unlocked settings read that locked would queue every apply behind every
// page view, which is what modules/site/internal/service.go says at the locked
// read. A reader racing a save sees revision N or N+1 and its own body, never a
// mixture, because one Settings call reads the whole row.
func (c composition) describe(ctx context.Context) (*screens.Connection, error) {
	tx, ok := httpx.TxFrom(ctx)
	if !ok {
		return nil, problem.New(http.StatusServiceUnavailable, "the database is not reachable right now")
	}
	settings, err := c.sites.Settings(ctx, tx)
	if err != nil {
		return nil, err
	}
	passkey, err := c.passkeys.PasskeySignInEnabled(ctx, tx)
	if err != nil {
		return nil, err
	}
	// The two provider reads answer nil for "this workspace names no provider",
	// which is also what a provider whose secret cannot be resolved reads as: the
	// column names where a secret lives, never the secret, so this document can
	// not leak one even by describing a half-configured provider.
	oidc, _, err := c.tenants.OIDCOf(ctx, tx)
	if err != nil {
		return nil, err
	}
	saml, _, err := c.tenants.SAMLSettingsOf(ctx, tx)
	if err != nil {
		return nil, err
	}

	// A colour the rule cannot read is an old row, not a bad request: the rule
	// refuses writes and never the read that finds something it cannot measure, so
	// both ratios are answered and the accent is answered exactly as stored. A
	// shell that wants to warn somebody reads light < 3 here.
	light, dark, _ := sitecontracts.AccentRatios(settings.PrimaryColor)
	out := &screens.Connection{
		Name:        workspaceName(settings, ctx),
		Accent:      settings.PrimaryColor,
		AccentRatio: screens.AccentRatio{Light: light, Dark: dark},
		Theme:       settings.Theme,
		Revision:    settings.Revision,
		Methods: screens.SignInMethods{
			// Four independent booleans, and no claim of exclusivity: a workspace
			// with a provider keeps its passwords working, and a shell that was told
			// otherwise would hide a door that still opens.
			Password: c.passwordDoor,
			Passkey:  passkey,
			OIDC:     oidc != nil,
			SAML:     saml != nil,
		},
		SignIn:   screens.SignInDoor{Address: c.signin.Address},
		Recovery: screens.RecoveryAvailability{Available: c.passwordDoor && c.recoveryMail},
	}
	if settings.LogoFileID != nil {
		// The same builder the web header composes its <img src> with, so the
		// answer is an address this installation serves rather than one this file
		// invented. A logo whose file has gone is no logoUrl at all: the id carries
		// no foreign key by decision, and a dangling one is a workspace shown
		// without a mark — not a 404 of the whole document and not a 500.
		out.LogoURL = c.links.PublicFile(settings.LogoFileID.String())
	}
	if c.signin.Registration != nil {
		// The composition's own value, verbatim: a shell never offers a form the
		// installation did not mount, because this file has no registration to
		// invent.
		out.SignIn.Registration = &screens.RegistrationDoor{
			Kind: string(c.signin.Registration.Kind), Address: c.signin.Registration.Address,
		}
	}
	return out, nil
}

// workspaceName is the name and its two fallbacks, which is the rule
// modules/web/internal/mount.go applies to the header a browser reads: the site's
// own title, then the tenant's name, then the installation's. That is what makes
// the reference tenant answer with a name on the day it is created, before anybody
// has configured a site, which is the difference between a first screen and a
// blank one.
func workspaceName(settings *sitecontracts.SiteSettings, ctx context.Context) string {
	if settings.Title != "" {
		return settings.Title
	}
	if t, ok := tenancy.FromContext(ctx); ok && t.Name != "" {
		return t.Name
	}
	return brandName
}
