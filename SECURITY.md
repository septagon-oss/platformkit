# Security policy

## Supported versions

The latest tag, and `main`. There is no long-term support branch: this is a
reference architecture, and a fix lands on `main` and in the next tag.

## Reporting a vulnerability

Do not open a public issue, and do not put a reproduction in a pull request.

- Use **GitHub private vulnerability reporting** on this repository
  (Security → Report a vulnerability). It is the preferred channel.
- If that is unavailable, email **security@septagon.dev**.

Include what makes a report actionable: the commit, the route or package, the
privileges the attack needs, minimal reproduction steps, and logs with
credentials, tokens and any tenant data removed. Say whether you believe it is
being exploited.

We will acknowledge a credible report, ask for detail if we need it, and agree
disclosure timing with you. There is no published SLA, because a reference
architecture with one maintainer that promised one would be lying.

## What is in scope

The kernel, the modules, the UI stack, the reference application, the gates and
the deployment files in this repository — and the documentation, when it is
wrong in a way that would make somebody deploy insecurely.

Three areas are worth naming because they are where the design puts its weight,
and a hole in one of them is a hole in the claim:

- **Tenant isolation.** A request that reads or writes another tenant's rows.
  ADR 0003 states exactly what is and is not claimed, including a documented gap
  (`TestARestoringEscapeIsNotCaughtByTheReread`) that is already known.
- **Authorization.** A route reachable without the permission it declares, or a
  permission satisfiable in a way ADR 0006 says it should not be — an operator
  permission answered from an ordinary tenant, in particular.
- **The bootstrap.** `platformkit bootstrap` runs with no caller to authorize
  it, and what makes that safe is that it refuses an installation that has a
  tenant. A way past that refusal is a serious report.

## What is not

- Findings against a deployment's own configuration: `config.example.yaml` ships
  `docs: false` and a development password, and a deployment that kept either is
  its own finding.
- Anything requiring the `postgres` superuser or shell access on the host. The
  application connects as `platformkit_app` on purpose; an attack that starts by
  being the database owner has already won.
- Missing hardening headers on a page that requires no session and holds no
  data, absent a demonstrated impact.
