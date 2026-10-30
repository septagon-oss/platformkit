package webhook

import (
	"fmt"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/jobs"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/personaldata"
	"github.com/septagon-oss/platformkit/modules/webhook/contracts"
	"github.com/septagon-oss/platformkit/modules/webhook/internal"
	"github.com/septagon-oss/platformkit/pkit"
)

// Module is outbound webhooks as the resolver sees it, for an application that
// names it in Use rather than building the manifest by hand: `Use(webhook.Module)`.
//
// It hands out no contract: nothing calls this module, it subscribes. What it
// cannot decide for itself is answered by three things. Which tenant to walk with
// its two jobs is the tenant module's answer to jobs.TenantLister, the same one
// audit and task take. Which event names may be put on an endpoint is the
// composition's own fact — the Catalogue port, written as a literal beside the
// literals the message catalogues already are — because no module that is not built
// last can enumerate what an application emits. Whether anybody is told when an
// endpoint stops is the deployment's, and Optional because a deployment that tells
// people through its own subscription to webhook.endpoint.disabled composes nothing
// here and loses nothing.
//
// The signing key is the deployment's, read from webhook.secret_key (which
// PLATFORMKIT_WEBHOOK_SECRET_KEY overrides) and never from a tenant's word. An
// installation that names no key is refused at the composition rather than at the
// first endpoint it registers: without the key no secret can be wrapped, and an
// endpoint nobody can sign for is an address a customer gave away for nothing.
var Module = pkit.NewModule("webhook", wire,
	pkit.Needs[jobs.TenantLister](),
	pkit.Needs[contracts.Catalogue](),
	pkit.Optional[contracts.Alerts](),
	pkit.Contributes[personaldata.Part](),
)

func wire(w *pkit.Wiring) (module.Module, error) {
	settings := pkit.Config(w, func(c config.Config) config.Webhook { return c.Webhook })
	key := []byte(settings.SecretKey)
	if len(key) != contracts.SecretKeyBytes {
		return module.Module{}, fmt.Errorf(
			"webhook: its signing key is %d bytes and it is %d; it is deployment configuration, written in webhook.secret_key or PLATFORMKIT_WEBHOOK_SECRET_KEY",
			len(key), contracts.SecretKeyBytes)
	}
	m, err := NewChecked(Deps{
		SecretKey:           key,
		Catalogue:           pkit.Get[contracts.Catalogue](w),
		Alerts:              pkit.Get[contracts.Alerts](w),
		Tenants:             pkit.Get[jobs.TenantLister](w),
		AllowLocalEndpoints: settings.AllowLocalEndpoints,
		RetentionDays:       settings.RetentionDays,
	})

	if err != nil {
		return module.Module{}, err
	}
	pkit.Put[personaldata.Part](w, internal.PersonalData())
	return m, nil
}
