package main

import (
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
// "en" is the source language: the code's sentences are English, so no layer ships
// an English file and an unsupported browser language answers from the text the call
// site passed. Which languages exist is the set of files these directories hold,
// and the loader refuses one that is short.
func catalogues() xtext.Catalog {
	return xtext.Load("en",
		page.Catalogue(),
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
		webhook.Catalogue())
}
