# Security

**How do I report a vulnerability?**

Report it privately. **Do not open a public GitHub issue for a security
vulnerability** — a public issue tells everyone about the problem before there is
a fix.

## How to report

Email **security@septagon.dev** with:

- a description of the issue and the impact you think it has,
- the steps to reproduce it (a minimal proof of concept helps a lot),
- the affected repo, version, or commit.

We will acknowledge your report, work with you on a fix, and credit you when the
fix ships if you would like the credit. Please give us a reasonable window to
address the issue before disclosing it publicly.

## Supported versions

PlatformKit is early — **v0.1.0**, our first public release; expect APIs to
move. There is no long-term
support window yet. Security fixes land on the `main` branch of the affected
repo. If you need stability today, pin to a specific commit and watch the repo
for security updates.

| Version | Supported |
|---|---|
| `main` (current) | Yes — fixes land here |
| Tagged early releases (v0.x) | Best effort; upgrade to current |

## Scope

This repo is the thin front door: a `main` wrapper over
`pk-apps/pkg/starterapp`. In scope: vulnerabilities in PlatformKit's own code —
this repo and the layers under the `septagon-oss` organization (`pk-core`,
`pk-modules`, `pk-runtime`, `pk-apps`, `pk-tools`, and the other published
layers), including the security baseline (CSRF, CORS, headers, password hashing,
signed cookies, rate-limiting, signature verification) and the module/port
boundary. If you can identify the owning layer, report it against that repo;
when in doubt, report it here and we will route it.

Worth knowing before you report:

- **The starter `/admin` dashboard is open by design.** It is not behind a login
  wall in the OSS starter; the seeded credentials authenticate against the auth
  API, not an admin login screen. An unauthenticated `/admin` in the starter is
  expected behavior for local development, not a vulnerability. Put it behind
  your own auth before exposing it.
- **SQLite is the local default, not a production-at-scale store.** "SQLite does
  not scale to a large production deployment" is a documented limitation, not a
  security bug — swap in your own store behind the store port for production.

Out of scope: issues in your own application code built on top of PlatformKit,
and issues in third-party dependencies (report those upstream, though we welcome
a heads-up).

---

See also:
[open-core.md](https://github.com/septagon-oss/pk-docs/blob/main/docs/v0.2.0/open-core.md)
for what the OSS security baseline covers,
[quickstart.md](https://github.com/septagon-oss/pk-docs/blob/main/docs/v0.2.0/quickstart.md)
for the honest caveat about the open `/admin` dashboard.
