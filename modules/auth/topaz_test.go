package auth_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/tenancy/providers/topaz"
	"github.com/septagon-oss/platformkit/modules/auth"
)

func TestTopazCompatibilityConstructor(t *testing.T) {
	var options auth.TopazOptions = topaz.Options{Path: "platformkit.task", Revision: "configured-v1"}
	if policy, err := auth.NewTopazPolicy(nil, options); err == nil || policy != nil {
		t.Fatalf("missing client accepted through compatibility constructor: %v, %v", policy, err)
	}
}
