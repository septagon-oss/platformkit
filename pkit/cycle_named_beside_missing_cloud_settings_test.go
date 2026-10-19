package pkit_test

// A deployment that both closes a cycle and names a cloud profile it has not
// filled hears both in one refusal: the cycle with the contract on each edge,
// and every setting the profile still owes. Build says what Validate says,
// returns no runtime and leaves the schema empty.

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/pkit"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/cart"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/collectibles"
)

func TestACycleIsNamedBesideTheSettingsACloudProfileStillOwes(t *testing.T) {
	cfg := onOneDatabase(t)
	cfg.Cloud.Profile = config.ProfileAWS
	d := pkit.Deployment{Environment: pkit.Staging, Config: cfg}
	compose := func() *pkit.App { return pkit.NewApp("collect").Use(cart.Module, collectibles.Module, doors) }

	validated := compose().Validate(d)
	says(t, validated, "Use: cart → collectibles → cart is a cycle: "+
		"cart takes cartcontracts.Extension from collectibles; collectibles needs cartcontracts.Service from cart")
	for _, key := range []string{"files.s3.bucket", "cache.url", "mail.host"} {
		if !strings.Contains(validated.Error(), "needs "+key+" in staging") {
			t.Errorf("the refusal names the cycle but drops the cloud setting %s:\n%v", key, validated)
		}
	}

	runtime, err := compose().Build(t.Context(), d)
	if runtime != nil {
		t.Cleanup(func() { _ = runtime.Close() })
		t.Error("a refused build returned a runtime")
	}
	says(t, err, validated.Error())
	if got := tablesIn(t, cfg.Database.MigrateURL); got != 0 {
		t.Errorf("the refused build left %d tables behind", got)
	}
}
