package httpx

import (
	"fmt"

	"github.com/septagon-oss/platformkit/kit/internal/syscap"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// systemTokenFor is the per-module mint: one cross-tenant credential per module
// that declared the need, carrying that module's own reason from its manifest,
// and answered from the same value on every later call — four registration passes
// read one composition, and two credentials for one module would be two answers
// to one question.
//
// It takes no reason, because the manifest is the only place the sentence is
// written: a caller allowed to name one could write its own audit record. A
// module that declared nothing gets nil and a recorded refusal naming it, so
// ValidateDeclarations answers every refusal at once and names the module — and
// nil is what makes the refusal honest: the capability is not handed out on the
// strength of a call nobody declared. See docs/adr/0006.
func (a *API) systemTokenFor(module string) tenancy.SystemToken {
	a.mu.Lock()
	reason, declared := a.declaredAccess[module]
	cached := a.systemTokens[module]
	a.mu.Unlock()

	if !declared || reason == "" {
		a.noteRefusal(fmt.Sprintf("httpx: module %q took a cross-tenant credential; it declares no SystemAccess — declare module.SystemAccess{Reason: what the work is} in the manifest, or do not take one", module))
		return nil
	}
	if cached != nil {
		return cached
	}
	return a.mintSystemToken(module, reason)
}

// mintSystemToken mints once, keeps the value, and marks the module as having
// taken what it declared. The mark answers the other half of the rule — a
// declaration nothing took is a manifest that no longer describes the code —
// which only becomes askable once every module's Routes has run, which is when
// ValidateDeclarations runs.
func (a *API) mintSystemToken(module, reason string) tenancy.SystemToken {
	token := syscap.NewSystemTokenFor(module, reason)
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.systemTokens == nil {
		a.systemTokens = map[string]tenancy.SystemToken{}
	}
	if a.tookAccess == nil {
		a.tookAccess = map[string]bool{}
	}
	// A module's Routes runs once per registration pass and every pass builds its
	// own API, so this is reached more than once for one module only across a
	// shared API. The first mint wins, so whoever asks twice holds one value.
	if prior, seen := a.systemTokens[module]; seen {
		return prior
	}
	a.systemTokens[module], a.tookAccess[module] = token, true
	return token
}

// untakenDeclarations is the half of the rule the mint cannot enforce at the
// door: a module whose manifest declared a cross-tenant need and never asked for
// the credential is a manifest that lies about the code, and the work written
// beside the declaration runs on nothing — a job that logs its way past a nil
// credential rather than refusing to start.
func (a *API) untakenDeclarations() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []string
	for module := range a.declaredAccess {
		if !a.tookAccess[module] {
			out = append(out, fmt.Sprintf("httpx: module %q declares SystemAccess and never took its token; take it in Routes where the work is, or drop the declaration", module))
		}
	}
	return out
}
