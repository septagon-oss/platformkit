package main

import (
	"embed"
	"io/fs"

	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
	"github.com/septagon-oss/platformkit/kit/richtext"
	"github.com/septagon-oss/platformkit/modules/admin"
	"github.com/septagon-oss/platformkit/modules/audit"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/billing"
	"github.com/septagon-oss/platformkit/modules/change"
	"github.com/septagon-oss/platformkit/modules/content"
	"github.com/septagon-oss/platformkit/modules/file"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/scim"
	"github.com/septagon-oss/platformkit/modules/site"
	"github.com/septagon-oss/platformkit/modules/subject"
	"github.com/septagon-oss/platformkit/modules/task"
	"github.com/septagon-oss/platformkit/modules/tenant"
	"github.com/septagon-oss/platformkit/modules/user"
	"github.com/septagon-oss/platformkit/modules/webhook"
	"github.com/septagon-oss/platformkit/ui/export"
	"github.com/septagon-oss/platformkit/ui/forms"
	"github.com/septagon-oss/platformkit/ui/page"
	"github.com/septagon-oss/platformkit/ui/resource"
)

// catalogues is this application's one message catalog: every copy source the
// composition holds, merged in the order the layers own their words.
//
// Kernel layers first, then the modules, then what this product writes of its own,
// then a client's — xtext.Load lets a later source answer for a key an earlier one
// carried, so the order is the merge rule and precedence is decided nowhere else.
// ui/page claims the `fault.` prefix as it is read, which keeps a product's
// catalogue from re-wording a kernel refusal; ui/resource claims nothing, so
// re-labelling "Delete" is a product's decision and not a pull request upstream.
//
// "en" is the source language: the sentences in the Go code are English, so no
// layer ships an English file, and an unsupported browser language is answered in
// English from the text the call site passed rather than from a catalog entry.
//
// The one language no file carries is en-XA, the pseudo-locale: this composition
// asks xtext to derive it from the English source copy, so every sentence the
// reference application can answer in arrives accented and longer than it is. That
// is a decision about this deployment — a client that ships no debugging language
// writes this line differently and nothing else changes — and it is the cheapest
// standing answer to "is this page actually translated", because a string that
// never went through a catalogue is the only kind of string that stays plain ASCII.
// A deployment that serves it to real customers has a problem; a deployment that
// cannot serve it has no way to see the bug.
// Which languages this deployment answers in is the set of files these directories
// hold — the gate that fails when one of them is short is the loader's own
// (kit/locale/providers/xtext), and the tenant's own supported set is negotiated
// on top of it in locale.go.
func catalogues() xtext.Catalog {
	return xtext.Pseudo(xtext.Load("en",
		page.Catalogue(),
		// The words of the gallery's frame — its landmark, its preview menus, the
		// button that applies a preview — belong to the layer that draws them.
		xtext.Source{FS: export.Catalogues(), Name: "ui/export"},
		xtext.Source{FS: forms.Catalogues(), Name: "ui/forms"},
		xtext.Source{FS: richtext.Catalogues(), Name: "kit/richtext"},
		xtext.Source{FS: resource.Catalogues(), Name: "ui/resource"},
		admin.Catalogue(),
		// Each module's own permission labels, so a refusal names a grant in the
		// language the tenant is served in and not in a key. Every module that
		// defines a permission ships words for it; the coverage case in
		// apps/platformkit is what makes shipping none a red test.
		audit.Catalogue(),
		auth.Catalogue(),
		billing.Catalogue(),
		change.Catalogue(),
		content.Catalogue(),
		file.Catalogue(),
		notification.Catalogue(),
		scim.Catalogue(),
		site.Catalogue(),
		subject.Catalogue(),
		task.Catalogue(),
		tenant.Catalogue(),
		user.Catalogue(),
		webhook.Catalogue(),
		// This product's own words, last: whatever a layer already owns it ships,
		// this holds only what no layer writes — the way out of the workspace this
		// composition names.
		productCatalogue()), "en-XA")
}

// productMessages is this composition's copy as files, one per locale. There is no
// en.json, and that is the loader's rule rather than an omission: the English of a
// label is the string the composition passes as the readable fallback, so the source
// language is worded where it is used and only the translation needs a file.
//
//go:embed messages
var productMessages embed.FS

// productCatalogue is the composition's own catalogue source: the words the reference product
// writes for a control no module owns. It claims no prefix — a client's catalogue is
// composed after this one, and re-wording what the reference product says is that
// client's ordinary decision — and a deployment that ships no file of its own reads
// the fallback in the Go source instead.
func productCatalogue() xtext.Source {
	files, err := fs.Sub(productMessages, "messages")
	if err != nil {
		panic("apps/platformkit: its messages directory is not in the binary: " + err.Error())
	}
	return xtext.Source{FS: files, Name: "apps/platformkit"}
}
