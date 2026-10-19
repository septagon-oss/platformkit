# Phone journey credential

Status: the route is decided by the implement branch's CI probe. The workflow
retains its existing secret-first fetch until that measurement. Secret rotation
and a passing journey with the selected credential remain unverified.

The reader is the fetch step in [mobile.yml](../.gitea/workflows/mobile.yml).
It resolves the pinned release, selects one asset by name, downloads its
published address, checks SHA-256 and exports a local file to the existing
[device harness](../scripts/mobile_e2e.sh). The same credential authenticates
both reads. The workflow's `contents: read` declaration does not restrict a
personal access token stored in `PK_MOBILE_TOKEN`.

## Previously measured boundary

GET `/api/v1/version` reported `1.27.3`. The following requests used the
workspace's existing `atlantis` credential; its scopes were not established.
These are measurements of routes, not proof of job-token or read-only access.

| Request | Response |
| --- | --- |
| `/api/v1/repos/septagon-oss/platformkit/releases/tags/journey-shell-mobile-31dc830` | 200 JSON; release 295, asset 30 |
| `/api/v1/repos/septagon-oss/platformkit/releases/295/assets/30`, with `Accept: application/octet-stream` | 200 JSON metadata, not APK bytes |
| `/api/v1/repos/septagon-oss/platformkit/releaseassets/30`, same Accept header | 404 |
| Published release download URL below | 200, 61,212,318 APK bytes, expected SHA-256 |
| `/attachments/67a9c8b3-c551-4707-9939-13a1d242e718` | 200 with workspace credential; anonymous request redirects to sign-in |

Measured published download URL:

```text
https://gitea.septagon.dev/septagon-oss/platformkit/releases/download/journey-shell-mobile-31dc830/platformkit-mobile-ci-x86_64-31dc830.apk
```

Measured SHA-256:

```text
e2166237a3733282112fdb66984c1fe8cf7e6c22dfb9655fd43521a4a05d1bfd
```

The single-attachment API metadata publishes `/attachments/<uuid>` whereas
the release-by-tag document publishes the release download URL. The diagnostic probes both
published addresses; do not infer their authorization from their names.

Existing [run 68240](http://192.168.1.200:3000/septagon-oss/platformkit/actions/runs/68240)
passed the fetch and failed the device harness. Its secret-first expression
does not identify the selected credential. It is not acceptance for this change.

## First measure the job token

The workflow step **Probe the pinned asset with only the job token** runs before
the real fetch. Its credential is `secrets.GITHUB_TOKEN` alone; it never reads
`PK_MOBILE_TOKEN`. The branch's own CI run supplies the measurement. Do not delete
or replace the current secret for this probe. Preserve `contents: read`.

Record the run URL, revision, forge version, credential source label, current
APK pin and expected digest. In that job context:

1. Read the release-by-tag API URL derived from `PK_MOBILE_APK`, using
   `Authorization: token <job token>`. Record the labeled HTTP status.
2. Resolve exactly one asset matching the pin's name. Read its advertised API
   metadata endpoint with `Accept: application/octet-stream`. The `asset API`
   status reports that request; on this forge its HTTP 200 body is metadata.
3. Read both the release document's `browser_download_url` and the
   single-attachment metadata's `browser_download_url`, with that same job
   token. Restrict authenticated requests to the pin's forge; never forward
   authorization to a different origin. The probe follows only same-origin
   redirects and prints status, byte count, digest and `pin_match`, never values
   of credentials, headers, response bodies or redirect destinations.
4. Hash actual downloaded bytes against the existing digest. A JSON response,
   login page, HTTP 200 alone or metadata-only success is not a successful
   asset read. Do not run installation when the hash differs.

The diagnostic always exits successfully, including on HTTP 401, 403 or 404;
the real fetch and journey still fail on errors. A transport failure, malformed
response or timeout is inconclusive. Save the run URL and each labeled outcome.

If `release bytes` or `attachment bytes` reports HTTP 200 and `pin_match=true`,
the next implementation round uses that measured route in the existing resolver
and binds only the job token. Update
the existing fixture and assertions to match the measured contract. Then the
owner removes `PK_MOBILE_TOKEN` from repository secrets and checks whether the
same name is inherited from organization or user secrets. Remove the inherited
grant for this repository too, without deleting another consumer's credential.
Run the final revision with the secret absent and require the full journey.

HTTP 200 from `release API` or `asset API` alone does not select the job-token
route. If the release API or the byte reads return 401, 403 or 404 without a
successful pinned-byte read, the owner route below stands. Record the probe's
run URL and the outstanding owner act: replace `PK_MOBILE_TOKEN` with a token
whose only scope is `read:repository`. Keep network failures inconclusive.

## Otherwise replace the personal access token

These steps apply only after a conclusive job-token diagnostic cannot obtain
the pinned bytes. The owner creates the token; the worker does not create an
owner credential or copy the workspace credential into a secret.

1. Confirm an existing bot account that can read `septagon-oss/platformkit`.
   The account `atlantis` exists, but this does not establish that its owner
   authorizes a new token. Prefer an approved existing bot with read access;
   otherwise use the owner's account as allowed by the brief. Do not grant
   repository administration or push access to make the download work.
2. As that account, open
   [User Settings → Applications](https://gitea.septagon.dev/user/settings/applications).
   Create a distinctly named token, for example `platformkit-mobile-apk-read`.
   In Select permissions set Repository to Read and every other category to
   No access. The resulting scope list must be exactly `["read:repository"]`:
   no `all`, write scope or additional read scope. Record the account, token
   name and displayed scopes without its value. See the
   [Gitea token instructions](https://docs.gitea.com/1.27/development/api-usage/#generating-and-listing-api-tokens).
3. Copy the one-time token directly into a password manager or the secret
   editor. In `septagon-oss/platformkit`, open Settings → Actions → Secrets,
   edit `PK_MOBILE_TOKEN`, replace its value and save. If the UI offers
   create/update instead of edit, save the same name with the replacement
   value. Verify the name and updated timestamp. Never paste the value into
   a command argument, issue, run log or committed file.
4. Keep the APK pin, digest and AVD unchanged. Dispatch `mobile.yml` on the
   reviewed implementation revision. For this validation bind the fetch
   credential directly to `secrets.PK_MOBILE_TOKEN`, without job fallback,
   so success demonstrates this token's read. Record only its source label.
5. Require both authenticated reads, digest verification, the harness's
   second digest check, successful `catalog` Maestro flow and the emitted
   JUnit report path. Record the run URL, revision and results under
   `Verified:`. A fetch-only success is insufficient.
6. On success, remove temporary diagnostics. The retained reader may use the
   existing secret-first expression. An absent secret may fall back to the
   job token, but must never fall back to a broader personal credential.
   The final revision must also pass the journey with the replacement secret.
7. The owner identifies the old token and its other consumers. Removing its
   value from this repository does not revoke it. Revoke a token dedicated
   to this journey; coordinate any shared token's other consumers before
   revocation. On a failure, keep the read-only replacement, correct the
   cause and rerun; do not restore broad authority as a workaround.

`read:repository` is a permission category, not proof of confinement to one
repository. Its reach also depends on the account's accessible repositories.
Use the bot's existing read access to limit reach; record any remaining reach
as an owner decision rather than claiming repository-only scope.

## Refusals and evidence

Correctable refusals: missing pin/digest, malformed pin, missing work directory,
absent/expired credential, denied read, missing/ambiguous asset, malformed
metadata, foreign download address, digest mismatch, unavailable runner tools
and a failing device flow. Stop before export/install for a failed fetch.
Correct the named input, access, published asset or environment and rerun.
Never accept a different digest merely to turn a failed run green.

Immutable refusal: a credential with any scope beyond `read:repository` is
ineligible for this task. Replace it; successful GETs cannot waive that rule.
An API metadata response is immutable evidence of that response, not bytes;
its status cannot be relabeled as a completed APK download.

The release can change between metadata and download. Keep both digest checks;
they refuse changed bytes. A running job can retain an old secret snapshot,
so start a fresh run after saving a replacement and distinguish concurrent
run IDs. Secret updates and workflow publication are not one transaction.

The known listener-attribution failure has its own owner. Obtain that fix in
the reviewed base before claiming a passing full journey; do not change token
scopes, APK pins or digest checks to hide it. Keep access evidence distinct
from a harness or Maestro failure.

## Reuse and limits

Composed from the existing workflow resolver, environment handoff, shell fetch
fixture, static checks, device harness, catalog flow and report checks.
This is the canonical credential procedure linked from the
[journey README](../e2e/maestro/README.md). The diagnostic is temporary; remove
it after the measured credential choice and fresh journey are verified.

No new module, library, SQL table, migration, domain command, event, UI, test
double or registry is required. Account custody, additional accessible
repositories, rotation schedule and shared-token revocation coordination
belong to the owner. Product authorization, tenant data, locale and money
are unchanged by the external artifact read.
