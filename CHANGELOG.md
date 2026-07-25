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

## [0.9.0] — 2026-07-25

### Changed

- **The whole console renders on the design system.** The admin shell's five
  text/templates and the sign-in page are retired in favor of typed Go views
  (`pk-modules` v0.11.0, `pk-apps` v0.10.0). Components — buttons, fields,
  tables, status pills, pagination — are `tw` class lists, the same layer any
  module admin page composes; the console's editorial voice stays product
  chrome aligned through the shared `--pk-*` tokens. The login page's palette
  is now generated from `themes.Default()` instead of hand-copied hex, and
  the admin script styles its runtime-built elements from the same compiled
  class strings via an embedded class-name bridge.
- Mobile resource tables scroll inside their shell with the header row
  visible, replacing the stacked-card transform.

## [0.8.0] — 2026-07-25

### Added

- **The design system is now the frontend baseline.** The admin stylesheet
  carries four layers — theme tokens (`pk-design`), role variables, one rule
  for every utility class `tw` can compile, and the bespoke shell rules — so a
  module admin page renders `pk-ui` components with no extra request, no build
  step, and no authored CSS. `reference/polls` ships the proof: a module-owned
  insights page, verified live at desktop and mobile widths.
- Three repositories join the public family: `pk-ui` (component contracts +
  renderers), `tw` (typed utility classes + CSS emission), `styleengine`
  (typed CSS engine).

## [0.6.2] — 2026-07-25

### Fixed

- The boot banner and the OpenAPI document now report the shipped release.
  Both said `0.4.0` while `@latest` installed v0.6.0: the version was a
  hand-maintained constant that survived two breaking releases unbumped. It is
  now single-sourced from `portslib.ReleaseVersion` (`0.6.0`), and the
  conformance tests pin the banner, module metadata, and `api/openapi.yaml`
  together so they cannot disagree again.

### Changed

- The admin console's design tokens now come from `pk-design`'s canonical
  theme, `themes.Default()`, instead of a literal private to the admin shell.
  The three type stacks join the token block as `fontFamily` tokens. Rendering
  is pixel-identical — verified by a line-set diff of the emitted custom
  properties and a screenshot comparison of the login page.

## [0.6.0] — 2026-07-25

Same code as 0.5.1. This release exists because the history of 0.4.0 through
0.5.1 was rewritten to correct commit attribution, which changed those commits'
identifiers. The old tags therefore no longer resolve to the content the Go
module proxy recorded, and are **retracted** in `go.mod`.

### Changed

- `retract [v0.4.0, v0.5.1]`. Pin `v0.6.0`. Versions `0.3.1` and earlier are
  unaffected and remain installable.


## [0.5.1] — 2026-07-25

### Fixed

- **The admin console works again.** 0.5.0 required the canonical opaque segment
  on every `/api/v1/<resource>/{id}` route, but the console's script still built
  those URLs with `encodeURIComponent`. The result was a console that could list
  entities and could not open, edit, delete, or publish any of them — every
  by-id call answered `400`. **Anyone running 0.5.0 should upgrade.**

  The coupling was invisible to Go tooling: `AdminResource.APIPath` is
  marshalled into the page and read back in the browser, so neither the Go
  source nor the script mentions `/api/v1` literally, and the blast-radius
  check for 0.5.0 missed it. A test now pins the two halves together and fails
  against the previous script.

### Added

- The boundaries section names what PlatformKit is not, relative to
  runtime-collection tools: schemas are Go modules compiled in, not collections
  defined at runtime through the admin UI.

## [0.5.0] — 2026-07-25

### Changed

- **Breaking — an entity identifier in a path is now one canonical opaque
  segment.** Every `/api/v1/<resource>/{id}` route accepts only the form
  produced by `pathsegment.EncodeOpaqueID`: the literal prefix `id-` followed by
  the lowercase-hex encoding of the identifier's bytes. A raw identifier returns
  `400` and the response names the expected form.

  ```bash
  ID='1784965307450776349-tenant_local-welcome'
  SEGMENT="id-$(printf '%s' "$ID" | od -An -tx1 | tr -d ' \n')"
  curl -s "http://127.0.0.1:8080/api/v1/content/$SEGMENT" -H "Authorization: Bearer $SID"
  ```

  This closes a real defect rather than tightening a rule for its own sake.
  `pk-client` already encoded identifiers this way while every handler read the
  path segment raw, so the two halves of the project disagreed on the wire
  format and **every by-id call from the client returned 404**. Both ends now
  share one implementation. Encoding also means an identifier containing a
  slash, a percent escape, or a control character cannot change which route a
  request resolves to, and an entity is reachable by exactly one spelling.

  A malformed segment answers `400`, not `404`: the request is malformed rather
  than naming something absent.

  Slugs are not identifiers and are never encoded — a route that addresses
  something by slug, such as a public page, stays readable.

  See [the API contract](https://septagon-oss.github.io/pk-docs/docs/current-api-contract/)
  for the full rule.

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
- Every PlatformKit dependency is pinned to a released tag. Earlier versions
  pinned pseudo-versions of `pk-apps` and `pk-modules`, so the module graph of a
  release could not be reproduced from tags alone.
- `modernc.org/sqlite` moves to 1.54.0.

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

[Unreleased]: https://github.com/septagon-oss/platformkit/compare/v0.8.0...HEAD
[0.9.0]: https://github.com/septagon-oss/platformkit/compare/v0.8.0...v0.9.0
[0.8.0]: https://github.com/septagon-oss/platformkit/compare/v0.7.0...v0.8.0
[0.6.2]: https://github.com/septagon-oss/platformkit/compare/v0.6.1...v0.6.2
[0.6.0]: https://github.com/septagon-oss/platformkit/compare/v0.5.1...v0.6.0
[0.5.1]: https://github.com/septagon-oss/platformkit/compare/v0.5.0...v0.5.1
[0.5.0]: https://github.com/septagon-oss/platformkit/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/septagon-oss/platformkit/compare/v0.3.1...v0.4.0
[0.3.1]: https://github.com/septagon-oss/platformkit/compare/v0.3.0...v0.3.1
[0.3.0]: https://github.com/septagon-oss/platformkit/compare/v0.2.3...v0.3.0
[0.2.3]: https://github.com/septagon-oss/platformkit/compare/v0.2.2...v0.2.3
[0.2.2]: https://github.com/septagon-oss/platformkit/compare/v0.2.1...v0.2.2
[0.2.1]: https://github.com/septagon-oss/platformkit/compare/v0.2.0...v0.2.1
[0.2.0]: https://github.com/septagon-oss/platformkit/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/septagon-oss/platformkit/releases/tag/v0.1.0
