package main

import (
	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
	"github.com/septagon-oss/platformkit/modules/admin"
	"github.com/septagon-oss/platformkit/modules/audit"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/billing"
	"github.com/septagon-oss/platformkit/modules/change"
	"github.com/septagon-oss/platformkit/modules/content"
	"github.com/septagon-oss/platformkit/modules/file"
	"github.com/septagon-oss/platformkit/modules/site"
	"github.com/septagon-oss/platformkit/modules/task"
	"github.com/septagon-oss/platformkit/modules/tenant"
	"github.com/septagon-oss/platformkit/modules/user"
	"github.com/septagon-oss/platformkit/ui/page"
	"github.com/septagon-oss/platformkit/ui/resource"
)

// catalogues is this application's one message catalog: every copy source the
// composition holds, merged in the order the layers own their words.
//
// Kernel layers first, then the modules, then anything this product writes of its
// own, then a client's — xtext.Load lets a later source answer for a key an
// earlier one carried, so the order is the merge rule and there is no second place
// where precedence is decided. `ui/page` claims the `fault.` prefix as it is read,
// which is what keeps a product's catalogue from re-wording what a kernel refusal
// says; `ui/resource` claims nothing, so re-labelling "Delete" is a product's
// ordinary decision and not a pull request into the foundation.
//
// "en" is the source language: the sentences in the Go code are English, so no
// layer ships an English file, and an unsupported browser language is answered in
// English from the text the call site passed rather than from a catalog entry.
// Which languages this deployment answers in is the set of files these directories
// hold — the gate that fails when one of them is short is the loader's own
// (kit/locale/providers/xtext), and the tenant's own supported set is negotiated
// on top of it in locale.go.
func catalogues() xtext.Catalog {
	return xtext.Load("en",
		page.Catalogue(),
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
		site.Catalogue(),
		task.Catalogue(),
		tenant.Catalogue(),
		user.Catalogue())
}
