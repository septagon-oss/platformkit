// Package translation is the module manifest: one table in which any field of
// any record can be said in another of the languages a tenant is served in.
//
// It is the shape every module follows — contracts/, internal/, and one
// exported function taking typed dependencies and returning a module.Module —
// with one difference worth naming before the code: this module owns no routes,
// no pages and no permissions of its own, and that is not an omission.
//
// A translation is reached from the record it translates, under `?lang=`, and
// it is written by the record's own write. So it is guarded by the record's own
// permission, audited by the record's own event, and drawn on the record's own
// screen. A permission of its own would be a second door to the same row, and a
// module that mounts a route here would be a module that can translate a record
// its caller cannot see.
//
// What it takes from an installation is therefore two lists somebody wrote
// down: which entities have translatable fields, and — optionally, and off by
// default — which machine translates text. The compiler checks both.
package translation

import (
	"github.com/septagon-oss/platformkit/kit/locale"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/modules/translation/contracts"
	"github.com/septagon-oss/platformkit/modules/translation/internal"
)

// Deps is what this module cannot make for itself.
type Deps struct {
	// Sources is one entry per entity that declares a translatable field, and
	// it is the only way this module learns that an entity exists. rest names
	// the fields, reads the rows through crud under the caller's transaction,
	// and is the reason "which records have no Portuguese yet?" can be answered
	// at all: that question is about another module's table, and a module that
	// reached across for it would be a module that could read another tenant's
	// rows by forgetting a WHERE.
	Sources []rest.TranslationSource

	// Translator is the optional machine. nil in every installation whose
	// operator named no provider — which is every installation until somebody
	// writes a URL down, because the default of a capability that publishes
	// unreviewed text under a tenant's name is off. With it nil, the suggest
	// route refuses, the button is not drawn, and no client object exists.
	Translator locale.Translator
}

// New is the manifest: the SQL, the one declared event, and the service a
// mounting Spec is handed. It declares no permission and mounts no route, and
// the paragraph above says why; it emits one event, and modules/audit's
// SubscribeAll is why no audit call appears anywhere in this module.
//
// The name is the one every module's constructor carries since the resolver
// arrived, because `Module` is the provider value beside this function — the
// declaration an application names in Use.
func New(deps Deps) (contracts.Service, module.Module) {
	svc := internal.NewService(deps.Sources, deps.Translator)
	return svc, module.Module{
		Name:          "translation",
		Migrations:    Migrations.Files,
		Adopts:        Migrations.Adopts,
		RulesFrom:     Migrations.RulesFrom,
		Permissions:   nil,
		Declared:      contracts.Events,
		Subscriptions: nil,
		Routes:        nil,
	}
}
