// Package contracts is everything another module, an app or a test may know
// about translations: the port a mounted resource knocks on, the event this
// module emits, and the two permissions. The implementation is in ../internal.
//
// One sentence decides the shape of this whole package, and it is worth saying
// before any type: a translation is not a record. There is no list route, no
// slug, no lifecycle and no `rest.Spec` here, because a translation is one
// *field* of a record in one language — a thing that goes stale on its own and
// that one person can review while another has not looked at the title yet. A
// person reaches it from the record, under `?lang=`, and the row is written
// there and nowhere else.
//
// So the port is declared in kit/rest, where the fields it translates live, and
// this package gives it a name of its own rather than a second copy of its
// types: two definitions of one interface are two that drift, and the drift is
// invisible until a caller compiles against the wrong one.
package contracts

import (
	"github.com/septagon-oss/platformkit/kit/rest"
)

// Service is this module's port: the door kit/rest opens for a field tagged
// `i18n:"translatable"`. It is rest.Translations, named here so that a consumer
// imports this package and not the kernel's HTTP layer, and so that the
// manifest, the fake and the real service all say the same thing.
//
// The tenant is never a parameter: it is the transaction's, and row-level
// security is what answers a wrong-tenant read with "not found" instead of with
// somebody else's row.
type Service = rest.Translations

// Re-exported so a caller that imports this package can name the states and the
// origins without reaching into kit/rest for one constant. These are aliases of
// the kernel's own values, not copies: there is one spelling of "outdated".
const (
	Missing   = rest.FallbackMissing
	Outdated  = rest.FallbackOutdated
	Machine   = rest.FallbackMachine
	Withheld  = rest.FallbackWithheld
	Removed   = rest.FallbackRemoved
	ByHuman   = rest.OriginHuman
	ByMachine = rest.OriginMachine
)
