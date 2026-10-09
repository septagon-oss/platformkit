package rest_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/rest"
)

func TestExplicitShownVisibilityOverridesHideListAtMount(t *testing.T) {
	defer func() {
		if fault := recover(); fault != nil {
			t.Fatalf("explicit visibility must override hide:list, but mount refused: %v", fault)
		}
	}()
	mountAs(t, rest.Spec[*shownAndHidden]{Module: "hinted", Entity: "hinted",
		Path: "/things", Read: "hinted:read", Write: "hinted:write"}, caller{})
}
