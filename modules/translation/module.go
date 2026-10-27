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
	"github.com/septagon-oss/platformkit/kit/personaldata"
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
//
// Deprecated: use NewChecked, which answers this refusal as an error.
func New(deps Deps) (contracts.Service, module.Module) {
	svc, manifest, err := NewChecked(deps)
	if err != nil {
		panic(err.Error())
	}
	return svc, manifest
}

// NewChecked builds the service and the manifest. A nil Translator is every
// installation whose operator named no provider: the suggest route refuses and
// the button is not drawn, so nothing is refused here and the error is nil for
// every Deps today. It opens nothing, runs no query and is safe to call twice.
func NewChecked(deps Deps) (contracts.Service, module.Module, error) {
	svc := internal.NewService(deps.Sources, deps.Translator)
	return svc, module.Module{
		Name:       "translation",
		Migrations: Migrations.Files,
		Adopts:     Migrations.Adopts,
		RulesFrom:  Migrations.RulesFrom,
		// The text an erasure reaches, and the two uuids it does not. A translation
		// is a field of somebody else's record, so `record_id` names a row of another
		// module and no person; `translator_id` names the person who wrote the text,
		// and `value`/`source_text` are the columns an identifier was copied into —
		// `source_text` is a copy of another module's field, address and all. The
		// answer this module gives for those two columns is internal/personaldata.go.
		PersonalData: personaldata.Implements(personaldata.Table{
			Name:      "translations",
			About:     []string{"translator_id"},
			Text:      []string{"value", "source_text"},
			NotPerson: []string{"record_id"},
		}),
		Permissions:   nil,
		Declared:      contracts.Events,
		Subscriptions: nil,
		Routes:        nil,
	}, nil
}
