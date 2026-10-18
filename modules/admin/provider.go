package admin

import (
	"github.com/septagon-oss/platformkit/kit/module"
	admincontracts "github.com/septagon-oss/platformkit/modules/admin/contracts"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
	"github.com/septagon-oss/platformkit/pkit"
)

// Module is the shell as the resolver sees it, for an application that names it
// in Use last: `Use(…, admin.Module)`.
//
// It runs after everything, and says so, because it generates a screen for every
// resource the other modules mounted: composed earlier it would generate screens
// for a prefix of the application, silently. `pkit.Composition` is where that
// list comes from now — the modules' own manifests, as the resolver built them —
// rather than a slice the composition happened to append to, which is the second
// answer to "what is in this application" this module's Deps used to require.
//
// It hands out nothing. The authorizer it filters its navigation with is the one
// the kernel enforces every route with, read from the module that answers it, so
// a menu and a door cannot disagree.
var Module = pkit.NewModule("admin", wire,
	pkit.Needs[authcontracts.Auth](),
	pkit.Needs[tenantcontracts.Service](),
	pkit.Optional[admincontracts.Signin](),
	pkit.Optional[admincontracts.Pictures](),
	pkit.Optional[admincontracts.Locale](),
	pkit.Optional[admincontracts.Storybook](),
	// The header's language control, and the two facts it draws: the person's own
	// choice through the account door, and the set this installation's copy is
	// written in. All three are optional in the shell's own words — a composition
	// that wires no account door draws no control rather than a broken one — and
	// each is answered by whoever owns the fact: the module that owns people for
	// the choice, the control plane for the set a tenant may be served in.
	pkit.Optional[admincontracts.AccountLanguage](),
	pkit.Optional[admincontracts.AccountLanguageVersion](),
	pkit.Optional[tenantcontracts.Languages](),
	pkit.After(pkit.Everything),
)

func wire(w *pkit.Wiring) (module.Module, error) {
	skin := w.Skin()
	auths := pkit.Get[authcontracts.Auth](w)
	signin := pkit.Get[admincontracts.Signin](w)
	return New(Deps{
		Modules:   pkit.Composition(w),
		Authorize: auths,
		Tenants:   pkit.Get[tenantcontracts.Service](w),
		// The three questions the shell asks are the same value seen through the
		// three interfaces it uses: what may this caller do, which roles exist,
		// which sessions are live.
		Roles:     auths,
		Sessions:  auths,
		Theme:     skin.Theme,
		Messages:  skin.Copy,
		Locale:    pkit.Get[admincontracts.Locale](w),
		Storybook: pkit.Get[admincontracts.Storybook](w),
		Language:  pkit.Get[admincontracts.AccountLanguage](w),
		// The counted door, from the same answer: a composition whose account door
		// counts its choices wires both, and one that wires only the intent door
		// draws no revision.
		LanguageVersion: pkit.Get[admincontracts.AccountLanguageVersion](w),
		// The languages this installation's copy is written in, which are the most a
		// tenant's own page may offer — the same value the control plane caps a
		// declaration with, read from the one port that carries it.
		Installed: pkit.Get[tenantcontracts.Languages](w).Tags,
		SignIn:    signin.Address,
		// A generated picture cell fetches through the file module's door, which this
		// shell does not mount and so cannot name: the composition that published that
		// door to the workspace catalogue hands it here as the same string.
		PictureSource: pkit.Get[admincontracts.Pictures](w).Source,
		// The form on the shell's login page posts to the auth module's door, and
		// the link to it is the same value, so neither half can offer what the
		// other did not mount.
		Registration: registration(signin.Registration),
	}), nil
}

// registration is the port's form choice read through the two lifecycles this
// shell renders. Nothing maps a word the shell does not render: an unknown kind
// is no form at all, which is the same answer a nil gave.
func registration(in *admincontracts.Registration) *Registration {
	if in == nil {
		return nil
	}
	out := &Registration{Address: in.Address}
	switch in.Kind {
	case admincontracts.KindEmail:
		out.Kind = RegistrationKindEmail
	case admincontracts.KindPassword:
		out.Kind = RegistrationKindPassword
	}
	return out
}
