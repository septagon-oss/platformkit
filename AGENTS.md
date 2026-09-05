# Working in PlatformKit

This repository owns the public runtime, reference modules, `design/`, `ui/`
and the reference application. Begin with [README.md](README.md).
[ARCHITECTURE.md](ARCHITECTURE.md) describes ownership and dependency boundaries;
[CONTRIBUTING.md](CONTRIBUTING.md) is the contribution policy. Read the relevant
sections before changing those boundaries.

Inspect `git status --short --branch` and preserve existing changes.
Find the owner in the implementation before adding a helper, interface or
configuration field. Module contracts live in `contracts/`, implementations in
`internal/`, and application constructors in `apps/platformkit/modules.go`.
UI work uses the existing `design/` and `ui/` packages.

Keep one logical change in one repository. Define public behavior and
independent conformance cases before implementation; remove the path a change
replaces. Do not create another registry, configuration namespace or generated
instruction document. Keep credentials, local configuration and generated
artifacts out of commits.

Run `make check` before committing and `make e2e` before pushing. The checks
use real development PostgreSQL and NATS services; `make up` starts them.
Read [Makefile](Makefile) before changing ports or invoking lifecycle commands:
`make down` deletes their volumes. Report actual commands and results, including
a failed or unavailable gate. Do not lower coverage, compress code or increase
budgets to make a check appear green.

For documentation work, verify links, source paths and command definitions.
Change the canonical explanation rather than copying it into every README or
agent file. Historical decision context is not an instruction to restore an
earlier implementation.
