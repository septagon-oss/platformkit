package httpx

import (
	"fmt"
	"regexp"
	"strings"
)

// addressClaim is one module's declaration of a protocol address.
type addressClaim struct {
	at      string
	surface Surface
	module  string
}

// jsonMediaType is the shape Options.JSONMediaTypes accepts: a type, a subtype,
// and the optional structured-syntax suffix a protocol names its JSON with. It
// is the grammar of RFC 9110's media types restricted to lower case, which is
// what the API compares a request's Content-Type against.
var jsonMediaType = regexp.MustCompile(`^[a-z0-9][a-z0-9!#$&^_.+-]*/[a-z0-9][a-z0-9!#$&^_.+-]*(\+[a-z0-9][a-z0-9!#$&^_.+-]*)?$`)

// validJSONMediaType answers whether a composition may say this media type is
// JSON. An empty entry is a composition that wrote a comma and meant nothing:
// refused, because an empty key in the format table matches nothing and the
// author would never hear about it.
func validJSONMediaType(mt string) bool { return jsonMediaType.MatchString(mt) }

// protocolPath accepts absolute, clean, lower-case paths made from the same
// segment characters as the composed surface prefixes.
var protocolPath = regexp.MustCompile(`^(/[a-z0-9][a-z0-9._-]*){1,}$`)

// roots are the addresses the table owns outright. A declared address may not
// be one of them, and none of them may be declared beneath: the same list
// relFault keeps a relative path away from, asked of the absolute side.
func roots() []string { return []string{apiRoot, AppRoot, OpsRoot, "/admin", "/public"} }

// At returns this router bound to an absolute address the module declares, for routes that
// speak a protocol naming its own address. Routes mounted on the returned router answer at
// <at>/… : the module writes /Users and gets /scim/v2/Users. The address is recorded against
// this module for the whole composition, so the request chain and the mount gate read one
// answer about it.
//
// The surface does not change, and that is the whole of why this is a door and not a hole:
// README.md, "Three surfaces". It is refused at mount, recorded so the process never listens,
// when the address is not clean, absolute, lower-case path form; when it names a prefix of the
// table or is named by one, which would put two owners on one half of the address space; or
// when another module already claimed it, in which case the message names both. Pages are not
// part of the declaration: a browser navigation belongs to the surface's own page prefix.
func (r *Router) At(at string) *Router {
	// A refusal hands back the plain router, so that the route the caller went
	// on to mount still lands somewhere ordinary and legible. The refusal is what
	// stops the process, not the address: a half-declared one would compose
	// nothing sensible and say less about it.
	claimed := &Router{surface: r.surface, module: r.module, api: r.api, owner: r.owner}
	if !protocolPath.MatchString(at) {
		r.api.noteRefusal(fmt.Sprintf(
			"httpx: module %q declared the address %q; a declared address is an absolute, clean, lower-case path of the form /name/version — no trailing slash, no pattern, no prefix",
			r.module, at))
		return claimed
	}
	for _, taken := range roots() {
		if at == taken || strings.HasPrefix(at, taken+"/") || strings.HasPrefix(taken, at+"/") {
			r.api.noteRefusal(fmt.Sprintf(
				"httpx: module %q declared the address %q, which is or is beneath %s; that half of the address space belongs to the surface table — declare an address the table does not already answer",
				r.module, at, taken))
			return claimed
		}
	}
	if fault := r.api.claimAddress(at, r.surface, r.module); fault != "" {
		r.api.noteRefusal(fmt.Sprintf("httpx: module %q: %s", r.module, fault))
		return claimed
	}
	claimed.at = at
	return claimed
}

// claimAddress records the declaration and answers why it cannot be recorded:
// an address is claimed once, by one module, with one surface, before the gate
// reads the table. Overlap counts as one claim: two modules that each declare
// half of `/scim/v2/…` would leave the chain asking which of them a request
// under it belongs to.
func (a *API) claimAddress(at string, surface Surface, module string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.frozen {
		return fmt.Sprintf("declared %q after the mount gate read the table; a declared address has to exist before anything can be refused about it", at)
	}
	for _, c := range a.addresses {
		if c.at == at || strings.HasPrefix(at, c.at+"/") || strings.HasPrefix(c.at, at+"/") {
			if c.module == module {
				if c.surface != surface {
					return fmt.Sprintf("%q is already declared by module %q on the %s router; one address answers on one chain", c.at, c.module, c.surface)
				}
				return "" // the same module may ask twice for the same door.
			}
			return fmt.Sprintf("%q is already declared by module %q; %q is one address and can have one owner", c.at, c.module, at)
		}
	}
	a.addresses = append(a.addresses, addressClaim{at: at, surface: surface, module: module})
	return ""
}

// surfaceOf is the request-time answer to "which surface is this address", and
// it is the only function allowed to answer that question about an address this
// composition declared. classify stays the pure function of the frozen table it
// is — three prefixes and two documented fallbacks, which every existing test
// reads — and this wraps it: a declared address, matched by longest prefix so a
// claim beneath a claim is impossible by construction, answers the surface of
// the router that declared it; everything else takes the table's answer.
//
// The table it reads is frozen by validateSurfaces, which runs before anything
// opens a port, so the answer a mount-time invariant checked is the answer a
// request gets. That is the whole reason the two cannot disagree.
func (a *API) surfaceOf(path string) Surface {
	a.mu.Lock()
	claims := append([]addressClaim(nil), a.addresses...)
	a.mu.Unlock()
	best, declared := "", Surface("")
	for _, c := range claims {
		if path != c.at && !strings.HasPrefix(path, c.at+"/") {
			continue
		}
		if len(c.at) > len(best) {
			best, declared = c.at, c.surface
		}
	}
	if best == "" {
		return classify(path)
	}
	return declared
}

// declaredPrefixes is the declared half of the address space, which a relative
// path is not allowed to name any more than it may name /api/v1.
func (a *API) declaredPrefixes() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]string, 0, len(a.addresses))
	for _, c := range a.addresses {
		out = append(out, c.at)
	}
	return out
}
