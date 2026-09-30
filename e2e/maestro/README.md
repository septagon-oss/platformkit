# The device journey

One flow, which this repository's own mobile job runs: a device signs in and reads
the workspace catalog at the address the kernel owns it at, then draws the screen one
of its entries named. It is the device-shaped half of the mobile contract; the other half is
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
which carries no `continue-on-error` anywhere. "Missing" means *nowhere it is looked
for*: `adb` and `emulator` are searched under `$ANDROID_HOME`, `$ANDROID_SDK_ROOT` and
`~/Android/Sdk`, and `maestro` under `$MAESTRO_HOME`, under the tools prefix this
repository's own tooling installs into (`~/.local/share/platformkit-tools/maestro/bin`)
and on `PATH`. A tool that exists and is not looked for is a journey that reports
itself impossible, so the search scope is part of the claim.

## The declared set, and the number

[flows.json](flows.json) is the declared set of mobile device flows — the denominator
of `mobile_flow_pass_rate`, written down rather than remembered. Each entry names the
repository that owns the spec: `platformkit` for the one flow here, `platformkit-mobile`
for the four that still run by hand in that repository.

`TestDeclaredMobileFlowsAreDeclared` (run by `make check`) refuses a flow that names no
spec, a spec this repository does not have, or an owning repository outside the
manifest's vocabulary. A flow owned by `platformkit-mobile` is counted in the
rate and never opened from here — this checkout cannot read that repository's files.
`TestDeclaredMobileFlowsRan` computes the rate over **every declared flow** — the
whole manifest is its denominator, not the one flow this job runs — and is run by
`scripts/mobile_e2e.sh` against the report that journey just wrote; a missing, failed
or skipped flow of *this* repository makes that job red.

The rate is `1/5`. The flow this repository owns passed on 2026-09-30 on a local
`x86_64` emulator (AVD `Expo_Pixel_8_API_36`, API 36) against a debug-signed shell
build whose SHA-256 begins `4ed0c4fc1754` and whose application id the flow asserts:
`catalog.yaml` signed in, read the catalog, and drew `resource-list` after tapping
`open-task-task`. The four flows the shell owns are in no report this job reads, which
is why the number is one-fifth and not one — it names what it is waiting on rather than
rounding itself up over the share it happens to run.

## What the published document does not carry yet

The contract at [testdata/openapi.json](../../apps/platformkit/testdata/openapi.json)
is what a shell is generated from today, and the brief's §1 asked it to carry two
things it does not. Both are named here so nobody reads the document as complete:

* **Locale negotiation.** No `Accept-Language` parameter, no `Content-Language` and no
  `Vary` header appears anywhere in the document (`If-None-Match` is the only header
  parameter published). `kit/locale.Select` answers inside the tenant's declared set
  on the wire, but the document cannot say so until `kit/httpx` can declare a response
  header — G7, deferred with that reason, and T-0111's owner to price. Item 4 of the
  work list below is the shell's share, and it is advice the document does not give.
* **The events a device may subscribe to.** The AsyncAPI document
  (`apps/platformkit/testdata/asyncapi.json`) names the payloads; nothing in the HTTP
  document points a device at a subscription address, because T-0109 has not mounted
  one. It joins the OpenAPI document in the commit that mounts the door — the rule
  `deviceContractPaths` in `openapi_contract_test.go` holds every other address to.

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

**Reused** — `apps/platformkit/asyncapi_test.go`'s golden-and-ratio shape,
`ui/screens.Describe` as the body of the address the flow taps, and the shell's own
`testID`s, unmodified, at its pinned revision. **Copied, not sourced** — `scripts/e2e.sh`'s
fixture (its own database, `bootstrap --tenant e2e --host localhost --language pt-PT`,
the built binary on a port, a trap that tears down whichever step failed) is
re-implemented in `scripts/mobile_e2e.sh`: 56 identical non-comment lines, and neither
script sources the other, so the next change to that boot sequence is a change in two
files until somebody lifts it into an owner of its own. **Added** — one flow of our own, because
no unit in either repository could state the claim that a device which signed in can
read this catalog and draw the screen its entry named: the shell's four flows exercise
the shell, and this repository had no device, no flow and no runner step at all. **Made
reusable** — `scripts/mobile_e2e.sh`, which the shell's own CI can copy step for step,
and the declared-flow manifest with its ratio test, which is the mechanism the next
device flow (or the bearer and push flows T-0117 and T-0113 will need) is added to.
