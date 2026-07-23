# Security

Report vulnerabilities privately to **hello@septagon.dev**. Do not open a
public issue before a fix is available.

Include the affected repository and commit or version, expected impact,
reproduction steps, and a minimal proof of concept when practical. Avoid
including real credentials or third-party personal data. We will acknowledge
the report, coordinate remediation and disclosure, and offer credit if wanted.

## Supported versions

PlatformKit is pre-1.0. Security fixes land on the current `main` branch of the
affected repository; tagged v0.x releases receive best-effort fixes. Consumers
should pin a version and monitor repository security updates.

| Version | Support |
|---|---|
| `main` | Current fixes |
| Latest tagged v0.x | Best effort |
| Older v0.x | Upgrade recommended |

## Security baseline

The runnable starter:

- binds to `127.0.0.1` unless an operator explicitly chooses another address;
- requires a production seed password outside development;
- never exposes credentials on the public landing or login pages;
- resolves tenant and subject from verified sessions or API keys;
- reserves interactive admin scopes from machine keys;
- requires `admin` plus `console:access` for the operator console;
- applies explicit read/write scopes to built-in data APIs;
- caps request bodies and protects process metrics;
- uses HttpOnly, SameSite session cookies and browser security headers.

Development mode intentionally seeds a known local credential and reasserts it
on boot. The default listener is loopback-only and prints a prominent warning.
Exposing development mode to a network is unsafe and is not a supported
deployment.

SQLite is the zero-setup default for local and small deployments. Production
operators remain responsible for transport security, secret injection, backups,
network policy, rate limiting across replicas, and an environment-specific
threat model.

## Scope

Reports are welcome for this repository and the published `septagon-oss`
layers, including authentication, authorization, tenant isolation, request
handling, migrations, cryptography, dependency wiring, and boundary failures.
If the owning repository is unclear, report it here and we will route it.

Issues solely in downstream application code or third-party projects should be
reported to their owners, though a heads-up is welcome when PlatformKit users
are materially exposed.
