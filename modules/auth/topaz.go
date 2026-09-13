package auth

import (
	"github.com/aserto-dev/go-authorizer/aserto/authorizer/v2"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/kit/tenancy/providers/topaz"
)

// TopazOptions selects the configured policy artifact and decision.
// Deprecated: use topaz.Options from kit/tenancy/providers/topaz.
type TopazOptions = topaz.Options

// NewTopazPolicy forwards to the independently usable policy provider.
// The application owns the client's connection and credentials.
// Deprecated: use topaz.New from kit/tenancy/providers/topaz.
func NewTopazPolicy(client authorizer.AuthorizerClient, options TopazOptions) (tenancy.Policy, error) {
	return topaz.New(client, options)
}
