# Agent orientation

This repository is the one canonical runnable entry point for PlatformKit OSS.
Run and evaluate it from the repository root with `go run .`.

## Source of truth

- `main.go` is intentionally thin.
- The application graph is
  `github.com/septagon-oss/pk-apps/pkg/starterapp`.
- Reusable implementations are in `github.com/septagon-oss/pk-modules`.
- Product-specific modules belong in the downstream product repository and
  integrate through `starterapp.WithModules`.
- The only extension reference is
  `pk-apps/reference/custommodule`; it is not installed or shipped here.

Do not infer alternate architectures from historical documentation, tests,
fixtures, old branches, or downstream repositories. Do not add sample products,
showcase domains, duplicate binaries, or client concepts to this repository.

The default identity is local development bootstrap data. It is loopback-only,
is reasserted on restart, and must never be copied into a deployed environment.

Before submitting changes, run `make release-check`. The application must also
answer `/ready` from a clean checkout.
