package internal

import (
	"context"
	"strings"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/locale"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// PasskeyWords is the copy the passkey doors refuse with, in the language this
// request asked to be answered in.
//
// Every refusal this module answers with is a sentence a person reads — a
// ceremony is browser-driven, so nobody parses these without eyes on them — and
// the sign-in page they appear on is translated: its title, its labels and the
// button beside them come from a catalogue and are answered in the tenant's
// language. A refusal that stayed in the source language would be the one
// Portuguese-speaking person's first wrong turn written in a language they did
// not ask for, on the one page this installation shows before it knows anything
// about them.
//
// The negotiation is the same contract every page of the shell uses — the
// caller's `Accept-Language` filtered against the languages the tenant is served
// in, the tenant's own default behind that — reached here through kit/locale
// rather than through ui/page, because a module's route has no page to render
// and the tenant is already on the request context, resolved from its host
// before this handler ran.
//
// What this deliberately does not do: name a `Content-Language`. The refusal is
// written by the error path of an operation, and nothing in the kernel lets a
// handler set a response header from there. The page the sentence lands on
// declares its own language (`<html lang>`), which is negotiated from the same
// header against the same tenant declaration, and ui/assets/js/passkeys.js
// carries that declaration onto the element it writes the refusal into.
//
// It is wired into the four sign-in legs, and not into the enrolment legs or the
// login mapping every credential route shares: the enrolment form sits on a
// screen whose copy this repository writes in English and declares as English
// (modules/admin/internal/sessions.go, `Language: writtenHere`), and /login's
// sentence is pinned to its element's declaration by
// apps/platformkit/review_r9_login_refusal_language_test.go. Naming those gaps
// and leaving them is the honest share of this change; closing them belongs to
// those screens and to every refusal in the module at once.
type PasskeyWords struct {
	// Messages is the application's whole catalogue, this module's entries
	// included. Nothing here is a composition of its own: the composition is
	// what knows every catalogue that ships (apps/platformkit/catalog.go), and a
	// module that loaded its own directory would answer a request in the one
	// language it happened to embed.
	Messages locale.Messages
}

// detail is refusal text for one key, in the negotiated language, with the
// English the Go source carries as the fallback the catalogue has no entry for.
func (w PasskeyWords) detail(ctx context.Context, key, english string) string {
	if w.Messages == nil {
		return english
	}
	r, ok := httpx.RequestFrom(ctx)
	if !ok {
		return english
	}
	// The caller's header first, whole: one header line is an ordered list with
	// quality values, and the provider reads the ranking. Handing it the list
	// split up would keep the tenant's set and throw the caller's order away.
	preferences := []string{}
	if asked := strings.TrimSpace(r.Header.Get("Accept-Language")); asked != "" {
		preferences = append(preferences, asked)
	}
	var served []string
	if t, here := tenancy.FromContext(ctx); here && t.Languages != nil {
		served = t.Languages.Preferred()
	}
	preferences = append(preferences, served...)
	if len(preferences) == 0 {
		return english
	}
	loc := w.Messages.Select(preferences...)
	if len(served) > 0 && !serves(served, loc.Language) {
		// The tenant's declaration outranks the deployment's catalogue: a tenant
		// served in two languages has not agreed to be refused in a third one
		// somebody else installed.
		loc = w.Messages.Select(served...)
	}
	if said := strings.TrimSpace(loc.Text(key, english)); said != "" {
		return said
	}
	return english
}

// refuse is one status, one key, and the sentence that key holds in the language
// this request is answered in.
func (w PasskeyWords) refuse(ctx context.Context, status int, key, english string) *problem.Problem {
	return problem.New(status, w.detail(ctx, key, english))
}

// serves says whether a tenant is served in a language at all, matching the way
// the tags are written on both sides rather than by parsing them here: the
// tenant's list and the provider's answer both come from the same canonical
// forms (modules/tenant validates the declarations, kit/locale canonicalises the
// negotiation).
func serves(served []string, language string) bool {
	for _, tag := range served {
		if strings.EqualFold(tag, language) {
			return true
		}
		if base, _, _ := strings.Cut(language, "-"); base != "" && strings.EqualFold(tag, base) {
			return true
		}
	}
	return false
}

// The keys this module's own catalogue carries words for. A key with no entry is
// no failure — the English in the Go source is the fallback, which is how every
// catalogue here works — so shipping a new refusal without its copy is a person
// reading English, not a broken page.
const (
	keyPasskeySignInOff      = "auth.refusal.passkey-sign-in-off"
	keyPasskeyPromptExpired  = "auth.refusal.passkey-prompt-expired"
	keyPasskeyNoAnswer       = "auth.refusal.passkey-did-not-answer"
	keyPasskeyTooManyPrompts = "auth.refusal.passkey-too-many-prompts"
	keyPasskeyOffPage        = "auth.refusal.passkey-answer-from-the-page"
)
