# Phase: implement, round 2 — task-T-0337-the-kernel-counts-what-each-resource-holds — T-0337-the-kernel-counts-what-each-resource-holds

The previous round did not pass. Read the gate output and the review below, fix every finding at its cause, keep the reviewer's failing tests and make them pass, and do not weaken a check to pass it. The reviewer's test files are uncommitted in the worktree: commit them **unchanged** in your fix commit (the gate hashes them and refuses a fix round that edited them), then make them pass by changing the code. If a finding is wrong, say why with evidence in `/home/jplr/.local/share/pkit-999/state/task-T-0337-the-kernel-counts-what-each-resource-holds/IMPLEMENT.md` — do not delete or edit the test.

WORKTREE: /home/jplr/pkit-999/worktrees/task-T-0337-the-kernel-counts-what-each-resource-holds
STATE: /home/jplr/.local/share/pkit-999/state/task-T-0337-the-kernel-counts-what-each-resource-holds

--- GATE OUTPUT ---
t.
|     planned_factor_names_its_task_test.go:21: go.mod names a WebAuthn library; the factor is no longer planned
|     inventory_go_test_ceiling_test.go:54: inventory quotes 172119 against 172119; declared go_test ceiling is 172855
| (the tail)
phase=expand duration_ms=1
2026/10/16 20:17:56 INFO db: applied migration owner=platformkit version=12 name=000012_operator.up.sql phase=expand duration_ms=2
2026/10/16 20:17:56 INFO db: applied migration owner=platformkit version=20 name=000020_tenant_primary_host.up.sql phase=expand duration_ms=2
2026/10/16 20:17:56 INFO db: applied migration owner=platformkit version=21 name=000021_limits.up.sql phase=expand duration_ms=3
2026/10/16 20:17:56 INFO db: applied migration owner=platformkit version=26 name=000026_module_schema.up.sql phase=expand duration_ms=1
2026/10/16 20:17:56 INFO db: applied migration owner=platformkit version=28 name=000028_outbox_trace.up.sql phase=expand duration_ms=1
2026/10/16 20:17:56 INFO db: applied migration owner=platformkit version=29 name=000029_tenant_locale.up.sql phase=expand duration_ms=5
2026/10/16 20:17:56 INFO db: applied migration owner=platformkit version=30 name=000030_tenant_oidc.up.sql phase=expand duration_ms=2
2026/10/16 20:17:56 INFO db: applied migration owner=platformkit version=34 name=000034_outbox_request.up.sql phase=expand duration_ms=0
2026/10/16 20:17:56 INFO db: applied migration owner=platformkit version=41 name=000041_outbox_baggage.up.sql phase=expand duration_ms=0
2026/10/16 20:17:56 INFO db: applied migration owner=platformkit version=43 name=000043_tenant_app.up.sql phase=expand duration_ms=4
2026/10/16 20:17:56 INFO db: applied migration owner=platformkit version=46 name=000046_tenant_saml.up.sql phase=expand duration_ms=2
2026/10/16 20:17:56 INFO db: applied migration owner=platformkit version=50 name=000050_tenant_demo.up.sql phase=expand duration_ms=1
2026/10/16 20:17:56 INFO db: applied migration owner=platformkit version=51 name=000051_seed_keys.up.sql phase=expand duration_ms=6
2026/10/16 20:17:56 INFO db: applied migration owner=platformkit version=52 name=000052_outbox_attribution.up.sql phase=expand duration_ms=3
2026/10/16 20:17:56 INFO db: applied migration owner=platformkit version=58 name=000058_ledger_claim_durable_lock.up.sql phase=expand duration_ms=3
2026/10/16 20:17:56 INFO db: applied migration owner=platformkit version=59 name=000059_tenant_app_refusal.up.sql phase=expand duration_ms=2
2026/10/16 20:17:56 INFO db: applied migration owner=platformkit version=60 name=000060_tenant_app_place.up.sql phase=data batches=0 duration_ms=6
2026/10/16 20:17:56 INFO db: applied migration owner=platformkit version=61 name=000061_ledger_placement_declares_its_tenants.up.sql phase=expand duration_ms=1
2026/10/16 20:17:56 INFO db: applied migration owner=platformkit version=62 name=000062_idempotency.up.sql phase=expand duration_ms=5
2026/10/16 20:17:56 INFO db: applied migration owner=platformkit version=67 name=000067_operations.up.sql phase=expand duration_ms=5
    moved_ledger_tenant_write_test.go:25: events: move the delivery ledger: a delivery is mid-claim on ledger-ledger-invoice_issued; no ledger row moved, run it again

DONE 6639 tests, 2 skipped, 2 failures in 675.754s
npm warn install-scripts 1 package had install scripts blocked because they are not covered by allowScripts:
npm warn install-scripts   esbuild@0.28.2 (postinstall: node install.js)
npm warn install-scripts
npm warn install-scripts Run `npm install-scripts ls` to review, or `npm install-scripts approve <pkg>` to allow.
test_citation_reader_refuses_missing_reversed_and_outside_ranges (__main__.ArchitecturePillars.test_citation_reader_refuses_missing_reversed_and_outside_ranges) ... ok
test_each_pillar_answers_the_five_questions_in_order (__main__.ArchitecturePillars.test_each_pillar_answers_the_five_questions_in_order) ... ok
test_full_and_shorthand_citation_ranges_resolve (__main__.ArchitecturePillars.test_full_and_shorthand_citation_ranges_resolve) ... ok

----------------------------------------------------------------------
Ran 3 tests in 0.019s

OK
+ export base 893cf2ae79a7f5b6a9b8618fd3586e237fea85ea
+ export candidate d5a15086867605ac21cdab0e046130622cdf63a6
make: *** [Makefile:543: check] Error 1

-> exit 2
$ make check
| failure lines above the tail below:
|     i18n_gate_test.go:130: publish = 409 {"$schema":"https://acme.localhost/schemas/Problem.json","type":"about:blank","title":"Conflict","status":409,"detail":"crud: conflict: the author of this content cannot publish it: publication is another person's decision","instance":"urn:request:497a02b8-ab
| # A stated per-package bound, because go test's ten-minute default is not a decision this
| # repository ever made and the suite grew past it. apps/platformkit's 122 cases each migrate an
| # installation into a schema of their own; the package measured 407s with three database packages
| # sharing one Postgres, and the run of 2026-10-06 killed it at 600s while it was still working —
| # six passing cases then reported as `(unknown)` over a goroutine dump of tests parked in
| # t.Parallel. A whole-suite run of 2026-10-06 on a host carrying six of this program's suites at
| # once took `modules/tenant/internal` to 18m37s and `kit/rest` — a package that opens no database
| # at all — to 20m, and the same five packages that failed there passed in 7m30s of wall clock
| # against 1m of CPU when nothing else was asking. Thirty minutes is the worst observation here plus
| # room for a machine doing other things as well. It is not a looser standard: the hang a bound
| # exists to catch still stops. Behind it the CI job that runs `make check` names 75 minutes for
| # itself, and the forge has three times cut that job off at about an hour over it — 94d387cc at
| # 3612s = 60m02s, b6f1e93 at 3612s, cc614f57 at 3609s, all three with 75 in their copy of
| # .gitea/workflows/ci.yml — so the bound that stops a hung package here is this line's, not the
| # job's, and the budget a step must fit is the runner's.
| # The count beside it is TEST_COUNT's (PR #141), and the two are written in this order because
| # scripts/ci_go_cache_test.sh reads the count variable as it sits directly after the option separator,
| # so the count goes first and the bound after it. A caller that empties TEST_COUNT for its own cached
| # runs then loses the count, and the line that comes back carries the bound on its own — two spaces
| # and all, because make substitutes nothing for an empty variable and leaves the separator on both
| # sides. The shell folds them; the pin in the arch probe is of the bytes the recipe emits, in the same
| # shape as the double space that probe already pins in the local goal's line. One caution for whoever
| # edits this paragraph next: that guard greps this recipe's text, so setting the separator and the
| # variable side by side anywhere in here would answer it from a comment.
| # Three files hold this command, and that is the price of a byte pin. The arch probe
| # scripts/check_architecture_test.sh dry-runs `make check` and `make test` under local selectors and
| # compares the fresh goal against a byte copy of it; scripts/make_check_count_default_test.sh asks
| # for the same bytes in CI's own shape, where nothing names TEST_COUNT; and scripts/ci_go_cache_test.sh
| # refuses a recipe that stops interpolating the count at all. All three were red on 2026-10-06, the day
| # -timeout=30m joined -count=1 here and nowhere else. Re-pinning is a decision somebody makes
| # rather than a fix: those cases refuse a re-pin that drops the count, the package pattern or any
| # stated bound, so what a re-pin may change is everything around the parts that make the run fresh.
| # The alternative is a fresh gate that narrows because nobody looked at it.
| (the tail)
/filetest (cached)
✓  modules/file/internal (cached)
∅  modules/notification
∅  modules/notification/contracts
✓  modules/notification/contracts/notificationtest (cached)
✓  modules/notification/internal (cached)
✓  modules/site (cached)
∅  modules/site/contracts
✓  modules/site/contracts/sitetest (cached)
✓  modules/site/internal (cached)
✓  modules/task (cached)
∅  modules/task/contracts
✓  modules/task/contracts/tasktest (cached)
✓  modules/task/domain (cached)
✓  modules/task/internal (cached)
✓  modules/tenant (cached)
∅  modules/tenant/contracts
✓  modules/tenant/contracts/tenanttest (cached)
✓  modules/tenant/internal (cached)
✓  modules/translation (cached)
∅  modules/translation/contracts
✓  modules/change (3.002s)
✓  modules/translation/contracts/translationtest (cached)
✓  modules/translation/internal (cached)
✓  modules/user (cached)
✓  modules/user/contracts (cached)
✓  modules/user/contracts/usertest (cached)
✓  modules/web (cached)
∅  modules/web/contracts
✓  modules/user/internal (cached)
✓  modules/web/internal (cached)
∅  modules/webhook
✓  modules/webhook/contracts (cached)
✓  modules/webhook/internal (cached)
∅  pkit/internal/fixture/admin
∅  pkit/internal/fixture/admin/contracts
∅  pkit/internal/fixture/audit
∅  pkit/internal/fixture/blog
∅  pkit/internal/fixture/cart
∅  pkit/internal/fixture/cart/contracts
∅  pkit/internal/fixture/collectibles
∅  pkit/internal/fixture/design
∅  pkit/internal/fixture/homepage
∅  pkit/internal/fixture/homepage/contracts
∅  pkit/internal/fixture/payment
∅  pkit/internal/fixture/payment/contracts
∅  pkit/internal/fixture/paypal
∅  pkit/internal/fixture/reports
∅  pkit/internal/fixture/rewards
∅  pkit/internal/fixture/shipping
∅  pkit/internal/fixture/shop
∅  pkit/internal/fixture/stripe
∅  pkit/internal/fixture/user
∅  pkit/internal/fixture/user/contracts
∅  pkit/internal/fixture/wishlist
✓  pkit (cached)
✓  tools/designexport (cached)
✓  tools/locbudget (cached)
✓  ui (cached)
✓  ui/components (cached)
✓  ui/components/examples (cached)
✓  ui/css (cached)
✓  ui/document (cached)
✓  ui/export (cached)
✓  ui/forms (cached)
✓  ui/icon (cached)
✓  ui/legible (cached)
✓  ui/page (cached)
✓  ui/resource (cached)
✓  ui/screens (cached)
✓  ui/source (cached)
✓  ui/style (cached)
✓  kit/events (1m15.075s)
✖  apps/platformkit (7m43.721s)

=== Skipped
=== SKIP: apps/platformkit TestDeclaredMobileFlowsRan (0.00s)
    mobile_flows_test.go:136: MAESTRO_JUNIT is unset: no device run happened here. make mobile-e2e runs one and sets it; the mobile job fails without it.

=== SKIP: modules/auth TestAPlannedFactorNamesTheTaskThatFinishesIt (0.00s)
    planned_factor_names_its_task_test.go:21: go.mod names a WebAuthn library; the factor is no longer planned

=== Failed
=== FAIL: apps/platformkit TestTheInventoryQuotesItsDeclaredGoTestCeiling (0.00s)
    inventory_go_test_ceiling_test.go:54: inventory quotes 172119 against 172119; declared go_test ceiling is 172855

DONE 6639 tests, 2 skipped, 1 failure in 463.721s
npm warn install-scripts 1 package had install scripts blocked because they are not covered by allowScripts:
npm warn install-scripts   esbuild@0.28.2 (postinstall: node install.js)
npm warn install-scripts
npm warn install-scripts Run `npm install-scripts ls` to review, or `npm install-scripts approve <pkg>` to allow.
test_citation_reader_refuses_missing_reversed_and_outside_ranges (__main__.ArchitecturePillars.test_citation_reader_refuses_missing_reversed_and_outside_ranges) ... ok
test_each_pillar_answers_the_five_questions_in_order (__main__.ArchitecturePillars.test_each_pillar_answers_the_five_questions_in_order) ... ok
test_full_and_shorthand_citation_ranges_resolve (__main__.ArchitecturePillars.test_full_and_shorthand_citation_ranges_resolve) ... ok

----------------------------------------------------------------------
Ran 3 tests in 0.020s

OK
+ export base 893cf2ae79a7f5b6a9b8618fd3586e237fea85ea
+ export candidate d5a15086867605ac21cdab0e046130622cdf63a6
make: *** [Makefile:543: check] Error 1

-> exit 2
$ python3 /home/jplr/.local/share/pkit-999/gates/commits_gate.py . origin/main
commit gate: ok (2 commits, 2 authored)

-> exit 0


--- REVIEW ---
(no review yet)

Start with the reviewer's failing tests: run each one by name and make it pass at its cause. Then rerun each worker-owned command listed at the end of this prompt under "THE COMMANDS THAT DECIDE THIS ROUND", in order (a command marked as run by the loop is the loop's), and amend or add commits; every commit keeps a `Verified:` paragraph, and rewrite `/home/jplr/.local/share/pkit-999/state/task-T-0337-the-kernel-counts-what-each-resource-holds/IMPLEMENT.md`. **Verify narrow first, then once in full.** For a reported failure, reproduce it with the narrowest relevant check (one package, `go test -run '<Name>' ./<pkg>`, the one script). After fixing its cause, rerun that check, then each worker-owned deciding command once against the final candidate. Rerun a check when a later change invalidates its result. Do not repeat an unchanged full-suite failure: record its command, its output and what remains, and stop. A full suite waits for a heavy test slot shared by every round on this machine; the wait is not a failure, but it runs on your clock.

Finish `IMPLEMENT: complete` when the implementation is finished and every worker-owned deciding command passed. Use `IMPLEMENT: blocked — <the decision needed from root>` only for a root decision. Otherwise commit a checkpoint, write what is unfinished and which command is still red into `/home/jplr/.local/share/pkit-999/state/task-T-0337-the-kernel-counts-what-each-resource-holds/IMPLEMENT.md`, and end without a completion or blocked marker. Commands explicitly assigned to the loop (marked so below) are the loop's to run.

`IMPLEMENT: blocked` means one thing: a decision only root can take, which you name. Running short of time, or having work left that you know how to do, is not blocked: commit what is done, write the remaining edits into `/home/jplr/.local/share/pkit-999/state/task-T-0337-the-kernel-counts-what-each-resource-holds/IMPLEMENT.md`, and end without a marker. `complete` claims every worker-owned deciding command passed; do not write it over a red one. Two blocked rounds in a row hold the task for root (tools/run.py `root_hold`), and a hold for work you could have done costs a day.
--- THE SHELF: WHAT ALREADY EXISTS (decision 0087's shelf rule) ---
Three layers, in this order, before a line of this task is written. The mistake this section exists to stop (2026-10-10): a module on catalogue **main** was taken for a module the products repository could **compose**, and a round was briefed to port four the catalogue already holds.

1. THE CATALOGUE — the kernel is UNDER the catalogue: it composes none of these, and the column says what septagon-clients can compose at the catalogue it pins (`v0.0.0-20260927064226-76edd8512315`), which is what a kernel module may be assumed to have downstream. 32 modules at main (`/home/jplr/.local/share/pkit-999/mirrors/platformkit-catalog`, `44dd250`).

   **Not at that pin**, so not importable from this branch: instrument, library, portfolio, progress, verification, video — read them at main, and ask for the pin to move before writing a replacement.
   **At the pin and gone from main** — do not build on these: work.

   | module | at pin | what it is |
   |---|---|---|
   | album | now | Album is a numbered collection: a set of numbered things, and who holds how many of each. |
   | booking | now | Booking owns persistent reservations for a room and, optionally, a practitioner. |
   | calendar | now | Calendar gives the planning modules one way to read a person's existing commitments and one way to hand a plan back for… |
   | capture | now | capture/contracts owns reusable field definitions and value validation for products that collect structured data. |
   | cart | now | Cart owns baskets: opening, filling, checkout and the sweep that abandons untouched ones. |
   | clinical_record | now | This catalog module owns immutable versions of clinical nodes, correction history, subtree reads and hash-chain verific… |
   | consent | now | This catalog module owns a subject's explicit grants, expiry and revocation. |
   | device | now | The device module owns tenant-scoped inventory, configuration history and expiring command intents. |
   | diecut | now | Die-cut turns one uploaded picture into cut-ready assets: for every subject it finds, a contour that hugs the subject… |
   | fleet | now | This catalog owner provides stable vehicles, operational network records and actual position history. |
   | guardian | now | This module owns staff-verified guardian relationships and the guardian's explicit, revocable choices about content. |
   | kitchen | now | Kitchen plans a household's meals from recipes, saved pantry stock and away dates, scales ingredients to the people eat… |
   | layout | now | Layout reads a designed document as a layout. |
   | learning | now | This module owns reusable curriculum, immutable releases, enrollment, evidence, staff review, credentials and course en… |
   | membership | now | Membership owns versioned member plans, applications, approval and dated room-tier access. |
   | payment | now | Payment takes and gives back money for what the application sells and reads the processor's webhooks. |
   | personal | now | personal is the family product: the /app/personal/family hub a household returns to, one form per planner under /app/pe… |
   | printrequest | now | This catalog module owns print-request specifications, quote snapshots, scheduling, lifecycle transitions and history. |
   | redemption | now | A place that hands something out for a printed code, the codes it issued, and the single spending of each one. |
   | rip | now | Rip is a sealed thing: something that holds other things, belongs to one holder, and may be opened exactly once. |
   | space | now | Space owns bookable room configuration and scheduling rules. |
   | template | now | A designed document becomes a **template** — a set of named, typed **slots** discovered from the design — and a product… |
   | trace | now | Trace cuts a subject out of a picture: a caller submits a photograph and gets the subject back on a transparent backgro… |
   | training | now | Training turns an exercise catalog into a week of workouts for the muscles, equipment, experience, goal, session count… |
   | trip | now | Trip builds day itineraries for a destination: places for the interests a person names, legs between them, visits withi… |
   | vacation | now | … |
   (`…` = this section ran out of room for that row's one line, not out of modules: every name above is on the shelf and the whole list is in `/home/jplr/.local/share/pkit-999/mirrors/platformkit-catalog/modules/`)
   no line for: instrument, library, portfolio, progress, verification, video — every one of them in `/home/jplr/.local/share/pkit-999/mirrors/platformkit-catalog/modules/`

2. THE OLD PLATFORM — four read-only repositories (decision 0087's shelf rule). Nothing here is a dependency: read it, port a module when an app pulls it (decision 0087's no-blind-bulk-port ruling), and never copy a former client's code into a generic module (decision 0038).
       git clone --depth 1 http://192.168.1.200:3000/atlantis/platformkit-business-modules.git
       git clone --depth 1 http://192.168.1.200:3000/platformkit/client-modules.git
       git clone --depth 1 http://192.168.1.200:3000/atlantis/platformkit-frontend-kit.git
       git clone --depth 1 http://192.168.1.200:3000/atlantis/platformkit-apps.git
   Line counts of the two module repositories (physical lines of *.go excluding *_test.go, walked recursively, no exclusions), generated 2026-10-10T10:57:52+00:00 from `atlantis/platformkit-business-modules`@`daaf89e5b`, `platformkit/client-modules`@`1d04033fe`. Production lines, tests beside them; the human verdict for each (in the kernel / in the catalogue / on the shelf) is decision 0087's own table, under `/home/jplr/gitrepos/pkit/ops/platformkit-record/architecture/999-apps/decisions/` — read it before you call one of these new.
   counts are 167 h old and may name modules that have moved since; refresh with `python3 tools/run.py shelf --refresh`.

   | module | lines | tests | repo |
   |---|---:|---:|---|
   | collectibles | 99,032 | 63,935 | client-modules |
   | auth | 50,877 | 44,451 | platformkit-business-modules |
   | admin | 23,666 | 12,426 | platformkit-business-modules |
   | notification | 19,080 | 12,450 | platformkit-business-modules |
   | design | 18,607 | 10,980 | client-modules |
   | design | 18,581 | 10,986 | platformkit-business-modules |
   | tenant | 18,404 | 10,926 | platformkit-business-modules |
   | user | 16,290 | 10,703 | platformkit-business-modules |
   | audit | 15,587 | 4,841 | platformkit-business-modules |
   | site | 15,295 | 5,913 | platformkit-business-modules |
   | content | 12,187 | 5,096 | platformkit-business-modules |
   | file | 11,741 | 8,340 | platformkit-business-modules |
   | stock_exchange | 9,172 | 6,296 | platformkit-business-modules |
   | stock_exchange | 9,041 | 6,188 | client-modules |
   | ports | 8,887 | 4,124 | platformkit-business-modules |
   no counts for: api_key, catalog, print_request, proxies, print_request, spatial, collect_brand, operator, billing, payment, cart, chat, infrastructure, translation, insights, change, stock_execution, mail, courses, order (+47 more) — every one of them in `/home/jplr/.local/share/pkit-999/state/shelf/counts.json`

3. THE RULE — check these three layers before you write; your SPECIFY.md states, per piece of what you build, which layer it comes from — catalogue, old platform, or new — and a piece marked new while the shelf holds it is a failed round. The brief gate refuses a new brief for these repositories whose `## Shelf` table names no layer per piece; whether a claim is *true* is the review round's to check, not a gate's.


--- THE COMMANDS THAT DECIDE THIS ROUND ---
The loop runs exactly these in your worktree when you stop, in this order, and the first that exits non-zero refuses the round. Run them yourself before you stop, read what each prints, and fix the cause of any failure. Never weaken a check to pass it. If one cannot pass for a reason outside this task, say so in your report with its output.

    make check
    make check
    python3 /home/jplr/.local/share/pkit-999/gates/commits_gate.py . origin/main


--- REVIEW-PINNED TESTS BESIDE WHAT YOU CHANGED ---
Tests a past review pinned (review…/round…/probe…_test.go) are a frequent cause of refused rounds. These run the pinned tests in the packages your branch changed; run them early and again before you stop. Never edit or delete a pinned test to pass it. A pinned test can also break from a change elsewhere, which the gate will show.
    go test -run '^(TestABootstrapThatRefusesALanguageWritesNoInstallation|TestALocaleRouteRefusesALanguageTheInstallationHasNoCopyFor|TestAPersonRefusedAScreenIsAnsweredInTheTenantsLanguage|TestAPersonWhoNavigatedAndWasRefusedIsAnsweredInTheTenantsLanguage|TestBootstrapSaysItsLanguagesOnce|TestEveryPageSaysTheSameLanguageTwice|TestTheReviewQueueListsAnOpenProposal|TestTheSignInRefusalIsDeclaredInTheLanguageItIsWrittenIn|TestTwoTenantsAreRefusedInTheirOwnLanguages|TestTwoTenantsOfOneInstallationAreServedInTwoLanguagesAtOnce)$' ./apps/platformkit
    go test -run '^(TestARefusalNormalisesItsHostBeforeAskingWhoseTenantItIs)$' ./kit/httpx
