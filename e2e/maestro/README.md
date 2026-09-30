# The device journey

One flow, run by this repository's own CI: a device signs in and reads the workspace
catalog at the address the kernel owns it at, then draws the screen one of its
entries named. It is the device-shaped half of the mobile contract; the other half is
the document at [testdata/openapi.json](../../apps/platformkit/testdata/openapi.json),
gated by [openapi_contract_test.go](../../apps/platformkit/openapi_contract_test.go).

```sh
make mobile-e2e                  # needs a device, the shell's pinned APK, and the tools below
PK_MOBILE_APK=https://…/shell.apk \
PK_MOBILE_APK_SHA256=<64 hex> \
PK_MOBILE_AVD=<avd name> \
make mobile-e2e
```

`PK_MOBILE_APK` and `PK_MOBILE_APK_SHA256` are both required: the kernel does not build
another repository's app, and it will not install a binary it cannot name. The digest
is checked before `adb install`. `PK_MOBILE_AVD` names the emulator the CI job boots;
the harness refuses to run without one attached rather than guessing. A host with no
`adb`, no `emulator`, no `maestro` or no readable `/dev/kvm` exits non-zero and names
the missing piece — see [.gitea/workflows/mobile.yml](../../.gitea/workflows/mobile.yml),
which carries no `continue-on-error` anywhere.

## The declared set, and the number

[flows.json](flows.json) is the declared set of mobile device flows — the denominator
of `mobile_flow_pass_rate`, written down rather than remembered. Each entry names the
repository that owns the spec: `platformkit` for the one flow here, `platformkit-mobile`
for the four that still run by hand in that repository.

`TestDeclaredMobileFlowsAreDeclared` (run by `make check`) refuses a flow that names no
spec, a spec this repository does not have, or an owning repository this loop has never
heard of. `TestDeclaredMobileFlowsRan` computes the rate over the flows this repository
runs and is run by `scripts/mobile_e2e.sh` against the report that journey just wrote —
a missing, failed or skipped flow makes that job red. Today: **1 of 5 declared flows is
run by CI** (was 0 of 5 before this delivery; the shell's four are on its own list,
below).

## What the shell still has to do

For `platformkit-mobile`, one line each. Nothing here was written into that repository,
which this loop does not dispatch into; the lines name symbols rather than line numbers
so they survive its next merge.

1. **Move the address.** Its catalog call goes to `/api/v1/admin/resources`
   (`src/effects/api.ts`); request `/api/v1/app/resources`. The old one answers today
   through a redirect row in `kit/httpx/aliases.go` that is dated for removal at v1.3.0
   in `CHANGELOG.md` — the release after which the call becomes a redirect into a 404.
2. **Consume the bearer flow.** Sign-in leaves a `__Host-session` cookie, which a React
   Native client on `http://` cannot rely on and a device backgrounded overnight cannot
   refresh. When T-0117's exchange lands, send `Authorization: Bearer …` on every
   `/api/v1/` call and read a 401 as "re-exchange once, then return to the server
   screen", not as a silent retry.
3. **Register for push** once per install and once per token rotation, in the tenant the
   shell is signed into, and unregister on sign-out — when T-0113 mounts the door. Never
   present a token another tenant ever held: the kernel will refuse it, and it should.
4. **Read the catalog's locale.** Send `Accept-Language` from the platform's language
   tags on every request and honour `Content-Language` / `Vary: Accept-Language` on the
   catalog response; `kit/locale.Select` answers inside the tenant's declared set and
   never outside it. The hardcoded English strings go the same way.
5. **Forward the request id.** Send `traceparent` (W3C Trace Context, `kit/trace`) on
   every write, and show the id from the `problem` body (RFC 7807, `kit/problem`) on a
   refusal, so the person reading the message holds the string an operator can search. A
   malformed `traceparent` is discarded and a fresh context minted — honouring a caller's
   broken id would join somebody else's trace to this request.
6. **Take the palette from the gated copy.** `tools/designexport/testdata/design-tokens.json`
   and its `.source.json` beside it, instead of running
   `go run ./tools/designexport | jq '{schema, themes}'` by hand: the projection now has a
   producer-side gate that refuses a stale copy, and `scripts/tokens.ts` keeps only the
   refresh step that records the commit and tag it was taken at.
7. **Put its own four flows behind this harness.** `e2e/flows/{sign-in,sign-out,gallery,record}.yaml`
   exist and `scripts/check_flows.ts` proves only that every `testID` a flow names appears
   in some source file — its own header says it cannot prove the flows pass — and
   `android.yml` builds an APK and stops. `scripts/mobile_e2e.sh` is the shape to copy.

## Reused, added, made reusable

**Reused** — `scripts/e2e.sh`'s fixture (its own database, `bootstrap --tenant e2e
--host localhost --language pt-PT`, the built binary on a port, a trap that tears down
whichever step failed), `apps/platformkit/asyncapi_test.go`'s golden-and-ratio shape,
`ui/screens.Describe` as the body of the address the flow taps, and the shell's own
`testID`s, unmodified, at its pinned revision. **Added** — one flow of our own, because
no unit in either repository could state the claim that a device which signed in can
read this catalog and draw the screen its entry named: the shell's four flows exercise
the shell, and this repository had no device, no flow and no runner step at all. **Made
reusable** — `scripts/mobile_e2e.sh`, which the shell's own CI can copy step for step,
and the declared-flow manifest with its ratio test, which is the mechanism the next
device flow (or the bearer and push flows T-0117 and T-0113 will need) is added to.
