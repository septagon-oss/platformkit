# Topaz policy provider

Use `topaz.New(authorizer.NewAuthorizerClient(conn), topaz.Options{...})` to
adapt the official authorizer client to `tenancy.Policy`, without importing
Auth or starting the PlatformKit application. Supply the deployed policy path,
configured revision and optional decision/timeout. The constructor performs
no network call. The application owns the connection, credentials and close.

The existing policy contract validates tenant-qualified resource facts before
an evaluation. Trusted tenant, actor and action facts remain separate from
resource attributes. Denial differs from provider failure; context cancellation
and the configured timeout bound each call. Revision records configuration,
not proof of the server's current policy revision.

Run `go test ./kit/tenancy/providers/topaz` from the foundation repository to
exercise the adapter against an in-process gRPC server. These tests cover the
wire envelope, denial, invalid responses, cancellation and timeout; they do
not qualify a deployed Topaz server or synchronize its directory.

The [Task policy example](../../../../modules/auth/policies/README.md) explains
composed authorization. Existing `auth.NewTopazPolicy` and `auth.TopazOptions`
forward here for compatibility. Construction-error prefixes now identify
`topaz`; the old compatibility import still carries Auth's other dependencies.
New provider consumers should import this package directly.
