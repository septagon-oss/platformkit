# Changelog

All notable changes to the PlatformKit front door are documented here. The
format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and this
project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

PlatformKit is pre-1.0: minor versions may carry breaking changes. Pin a version.

This repository is the front door. The composition it runs lives in
[`pk-apps`](https://github.com/septagon-oss/pk-apps) and the modules in
[`pk-modules`](https://github.com/septagon-oss/pk-modules); their changelogs
cover changes to the module surface itself.

## [Unreleased]

## [0.4.0] — 2026-07-25

### Changed

- **Breaking — the local development identity changed.** The seeded tenant is now
  `tenant_local` (was `tenant_acme`), the administrator is
  `operator@local.test` (was `admin@local.test`), and the password is
  `local-development-only` (was `changeme`). Scripts that hardcoded the old
  values must be updated. The banner prints the current values on every boot.
- The front door is explicitly domain-neutral. It composes the nine-module
  starter and nothing else; sample domains and showcase products are not shipped
  here. Extension references live in `pk-apps/reference/`.
- `main.go` is thinner: address overrides moved into
  `starterapp.ApplyAddressOverrides`, and startup is injectable so the front door
  is testable without binding a port.
- The dev-mode banner now states plainly that the built-in password is
  re-asserted on every boot and that the process is not safe to expose.

### Added

- A second extension reference,
  [`pk-apps/reference/polls`](https://github.com/septagon-oss/pk-apps/tree/main/reference/polls),
  showing the `WithModules` seam carrying a full domain: append-only migrations
  with legacy-schema adoption, a draft → published → closed → archived
  lifecycle, author ownership plus a moderator scope, an audit outbox committed
  atomically with each mutation, server-signed anonymous voter identity,
  per-network throttling, `/metrics` counters, and a public browser surface
  beside the JSON API.
- `make release-check`, `make coverage`, and `make security` (govulncheck +
  gosec) gates, plus release analyzers that fail closed rather than warn.
- CI on every pull request: build and test, race, coverage, CodeQL,
  govulncheck, gosec, dependency review, and Dependabot updates.
- `AGENTS.md`, recording the canonical entry point and the boundaries that
  changes must respect.
- `NOTICE` and this changelog.

### Fixed

- Extension-owned data is preserved across an upgrade rather than dropped by
  bootstrap migration.
- Oversized request bodies return a clean `413` instead of a broken response.
- The documented scope of the project is now stated: there is deliberately no
  Storybook, component gallery, Figma export, renderer adapter, or Tailwind
  generation in the OSS surface.

## [0.3.1] — 2026-07-22

### Fixed

- Oversized bodies return a clean `413`.
- The startup banner lists contributed modules.

## [0.3.0] — 2026-07-22

### Changed

- Tracks `pk-apps` 0.3.0.

## [0.2.3] — 2026-07-22

### Changed

- Tracks `pk-apps` 0.2.3.

## [0.2.2] — 2026-07-21

### Security

- Hardening pass across the starter surface.

## [0.2.1] — 2026-07-21

### Security

- **Breaking** — requires `pk-apps` 0.2.1, carrying security-review hardening.

## [0.2.0] — 2026-07-21

### Changed

- **Breaking** — requires `pk-apps` 0.2.0. Requests are authenticated and
  tenant-scoped; tenant identity is derived from the verified credential rather
  than the request body.

## [0.1.0] — 2026-06-29

### Added

- First public release: the runnable front door for the nine-module starter over
  SQLite, on a loopback-only listener.

[Unreleased]: https://github.com/septagon-oss/platformkit/compare/v0.4.0...HEAD
[0.4.0]: https://github.com/septagon-oss/platformkit/compare/v0.3.1...v0.4.0
[0.3.1]: https://github.com/septagon-oss/platformkit/compare/v0.3.0...v0.3.1
[0.3.0]: https://github.com/septagon-oss/platformkit/compare/v0.2.3...v0.3.0
[0.2.3]: https://github.com/septagon-oss/platformkit/compare/v0.2.2...v0.2.3
[0.2.2]: https://github.com/septagon-oss/platformkit/compare/v0.2.1...v0.2.2
[0.2.1]: https://github.com/septagon-oss/platformkit/compare/v0.2.0...v0.2.1
[0.2.0]: https://github.com/septagon-oss/platformkit/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/septagon-oss/platformkit/releases/tag/v0.1.0
