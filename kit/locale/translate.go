package locale

import (
	"context"
	"errors"
)

// ErrNoProvider says this installation serves no machine translation at all.
// A double that is wired but switched off answers with it, which is what lets a
// test composition and an installation with no configured provider get the same
// refusal — the 422 that names the setting, not the 503 that names a failure.
var ErrNoProvider = errors.New("locale: no machine translation is configured")

// Translator asks an external service for a draft of text in another language.
//
// It is one method, because that is the whole request, and it is optional, since
// a machine translation nobody asked for is a liability: with no translator a
// composition has no client object, draws no button, and answers the route with a
// refusal naming the setting that would make it exist. The interface stays nil
// until an operator writes a provider's URL down.
//
// A saved machine draft is labelled and never served on a public page until
// somebody marks it reviewed — enforced where the row is read, not here, so no
// caller can forget it.
//
// The provider is a separate program over HTTP, which keeps an AGPL service out
// of this Apache-2.0 binary: a URL and a JSON body are the whole coupling.
type Translator interface {
	// Translate returns the text as the provider renders it in the target
	// language. An error is the provider's own words where it has any: the
	// caller shows them and writes nothing.
	Translate(ctx context.Context, text, from, to string) (string, error)
}
