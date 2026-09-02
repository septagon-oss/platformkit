# PlatformKit

Reference architecture for composable, multi-tenant SaaS in Go: one binary,
explicit wiring, Postgres row-level security, generated admin screens, ten CI
gates. Read [ARCHITECTURE.md](ARCHITECTURE.md).

## Run it (five commands)

```sh
git clone https://github.com/septagon-oss/platformkit && cd platformkit
make up
cp config.example.yaml config.yaml
make run
open http://platformkit.localhost:8080
```

## Status

Being extracted from a larger private codebase; see [docs/adr](docs/adr) and the
line budget in [loc-budget.json](loc-budget.json).
