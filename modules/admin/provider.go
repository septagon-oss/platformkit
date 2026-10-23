package admin

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	admincontracts "github.com/septagon-oss/platformkit/modules/admin/contracts"
	"github.com/septagon-oss/platformkit/modules/admin/internal"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
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
	// The tenant's own language door, which is the tenant module's second answer and
	// not a method on the first: the Languages page writes the declaration from inside
	// the tenant's transaction, and the shell asks for that door by name.
	pkit.Needs[tenantcontracts.LanguageDeclaration](),
	// Optional, not Needs: a composition with no user module has no names to show,
	// and the header says "Signed in" rather than the application refusing to boot.
	pkit.Optional[usercontracts.Service](),
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
	// The one primary action a list offers, which the composition resolves from the
	// modules it was built with rather than the shell guessing at them.
	pkit.Optional[admincontracts.PrimaryAction](),
	pkit.After(pkit.Everything),
)

func wire(w *pkit.Wiring) (module.Module, error) {
	skin := w.Skin()
	auths := pkit.Get[authcontracts.Auth](w)
	signin := pkit.Get[admincontracts.Signin](w)
	primary, err := primaryAction(pkit.Get[admincontracts.PrimaryAction](w), pkit.Composition(w))
	if err != nil {
		return module.Module{}, err
	}
	return New(Deps{
		Primary:   primary,
		Modules:   pkit.Composition(w),
		Authorize: auths,
		Tenants:   pkit.Get[tenantcontracts.Service](w),
		// The same service seen as the tenant's own language door.
		TenantLanguages: pkit.Get[tenantcontracts.LanguageDeclaration](w),
		// The three questions the shell asks are the same value seen through the
		// three interfaces it uses: what may this caller do, which roles exist,
		// which sessions are live.
		Roles:    auths,
		Sessions: auths,
		// The fourth question is who this is, and unlike the three above it needs an
		// adapter: the shell asks about the caller by id and gets back the two words
		// a header can say, not a row. Eight lines, and they are the whole of what
		// this module knows about the user module.
		People:    callerOf(pkit.Get[usercontracts.Service](w)),
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
		PictureSource:  pkit.Get[admincontracts.Pictures](w).Source,
		ComposedSource: pkit.Get[admincontracts.Pictures](w).ComposedSource,
		// The form on the shell's login page posts to the auth module's door, and
		// the link to it is the same value, so neither half can offer what the
		// other did not mount.
		Registration: registration(signin.Registration),
	}), nil
}

// callerOf is the header's read of the caller's own account row, or nothing at all
// when this application composes no user module — which is what leaves the frame to
// say "Signed in" rather than to fail.
func callerOf(users usercontracts.Service) People {
	if users == nil {
		return nil
	}
	return people{users: users}
}

type people struct{ users usercontracts.Service }

// Person reads one row: this caller's, in the transaction their request already
// holds. The tenant is not an argument, because the transaction is the tenant and
// row-level security has already said which rows are in it; an account of another
// tenant answers "not here", which the frame says as "Signed in".
func (p people) Person(ctx context.Context, userID uuid.UUID) (internal.Person, error) {
	tx, live := httpx.TxFrom(ctx)
	if !live {
		return internal.Person{}, errors.New("admin: this request carries no transaction to read the caller in")
	}
	user, err := p.users.Get(ctx, tx, userID)
	if err != nil {
		return internal.Person{}, err
	}
	return internal.Person{DisplayName: user.DisplayName, Email: user.Email}, nil
}

// primaryAction is the port's action checked against the composition, before
// anything opens: the zero value is no action; an action with no words, no
// permission, or a permission no composed module defines is refused, in the
// words kit/module refuses such a nav entry with.
func primaryAction(in admincontracts.PrimaryAction, mods []module.Module) (module.NavEntry, error) {
	switch {
	case in.Screen == "":
		return module.NavEntry{}, nil
	case in.Label == "":
		return module.NavEntry{}, fmt.Errorf("admin: the primary action for screen %q has no label; a button with no words is not an action", in.Screen)
	case in.Permission == "":
		return module.NavEntry{}, fmt.Errorf("admin: the primary action %q declares no permission; a link everyone sees is still a decision", in.Label)
	}
	for _, g := range module.Grants(mods) {
		if g.Permission == in.Permission {
			return module.NavEntry(in), nil
		}
	}
	return module.NavEntry{}, fmt.Errorf("admin: the primary action %q requires permission %q, which no module defines", in.Label, in.Permission)
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
