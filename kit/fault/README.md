# Refusals

`kit/fault` names the three failures a caller can act on: `ErrNotFound` when
there is no row this tenant may see, `ErrInvalid` when what was sent cannot be
accepted as written, and `ErrConflict` when the write contradicts data or state
that already exists. Two refusals sit outside the three by design: the ones
`kit/tenancy` declares are answered a 403 or a 503 with a code of their own,
because nothing the caller sent was wrong and the platform could not decide.
Anything else the mapping receives is an outage — the caller did nothing wrong
and the platform could not answer — which reaches a 500 with its cause in the log
and nothing in the body. Wrap one with the sentence a person can act on, with
the refusal at the front of it —
`fmt.Errorf("%w: a role name is at most %d characters", fault.ErrInvalid,
MaxRoleName)` — and the order is not style: `kit/rest` derives the message a form
marks against a control by trimming the literal `crud: invalid: ` off the front of
the problem's detail, so a refusal wrapped the other way round carries the marker
into the sentence a person reads, which is what `modules/admin` refuses on their
behalf. Every wrap of these three values in this repository's shipping source puts
the refusal first. A bare sentinel carries nothing to correct, and a fourth
sentinel would be a new answer to what a caller can act on, which is a decision
and not a convenience.

The package exists because of one link. A module's `contracts/`, `events/` or
`domain/` package refuses a write before any transaction exists, and `kit/crud` —
gorm, a driver, a `db.Tx` in every signature — was the only owner of these values.
This package imports `errors` and nothing else, so naming a refusal adds no
transaction to the package that refuses one. `kit/crud` re-exports the same three
values rather than declaring copies, so `errors.Is` matches whichever name a
caller wrote and `rest.Fault` maps the three to 404, 422 and 409 once for either.
What a client then reads is the mapping's decision, not this package's: a bare
`ErrInvalid` and a bare `ErrConflict` arrive as the problem's detail with their
message whole, a 404 is answered with the sentence `kit/rest` holds for it, and
only the `crud: invalid: ` prefix has a second package parsing it. See the
package doc.

## The limit, and what holds it

```sh
go test ./kit/fault/... -count=1   # fault_test.go: go list -deps lists one line, itself
./scripts/check_packages.sh        # check("kit/fault", ""): no non-standard dependency
```

Both fail rather than skip: the allowance refuses the first non-standard line in
the closure, and the architecture fixture
(`scripts/check_architecture_test.sh`) proves that refusal on a synthetic
`kit/fault` reaching `kit/db`. The edge back into `kit/crud` never reaches a
gate — that package imports this one, so the direction is an import cycle the Go
compiler refuses — and the message texts are interface: see the package doc, and
`TestSentinelsAreTheSharedValues` in `kit/crud`, which refuses a rename.

### Built on what came before

Decision 0022 asks a delivery to name what it composed rather than what it
rebuilt. **Reused:** the three values and their exact messages, moved out of
`kit/crud` rather than copied; `kit/crud`'s `Classify`, `UniqueConflict` and five
operations, which this package leaves alone; `pgconn.PgError`,
`gorm.ErrRecordNotFound` and `rest.Fault` in the new cases instead of new
doubles; and the `check` function already in the package gate, with an empty
allowance. **Added:** this package, because no existing unit could carry it —
`kit/problem` reaches huma, `kit/internal/syscap` sits under `internal/` and so
may not be imported by a module, `modules/task/domain` belongs to one module, and
`kit/entity`, which links only `uuid`, could have hosted the three but holds field
metadata, which is not the same decision. **Made reusable:** the refusals as a
value package's own, which is what decision 0024's rule R2 needs a `contracts/`
package to pass, and an empty allowance as a recorded boundary.

### Limits

No dependency count moves here, and this guide does not claim one. Eleven
`contracts/` packages of this repository wrap one of these refusals under the
adapter's name, and a guide that traced one has to name the rest. Three are a
module's own contract source: `modules/content/contracts`, whose `Render` wraps
`crud.ErrInvalid` for a body goldmark cannot read; `modules/file/contracts`,
whose `Agrees` wraps it for bytes that are not the type they were uploaded as;
and `modules/user/contracts`, which derives its own `ErrRegistrationExists` from
`crud.ErrConflict`, so `errors.Is` answers it under either name. Eight are the
shipped test-support package beside a module's contract, which refuses the way
the service it stands in for refuses, under the same name:
`modules/billing/contracts/billingtest`, `modules/content/contracts/contenttest`,
`modules/file/contracts/filetest`,
`modules/notification/contracts/notificationtest`,
`modules/site/contracts/sitetest`, `modules/task/contracts/tasktest`,
`modules/tenant/contracts/tenanttest` and `modules/user/contracts/usertest`.
`modules/auth/contracts` is on neither list: this change moved it to
`fault.ErrInvalid`, and no file of its own imports `kit/crud` now. None of the
eleven moves a closure: each names `db.Tx` in its own files and imports
`kit/crud` or `kit/db` for `crud.Base` and a transaction-aware service, so
`go list -deps` still lists gorm and `database/sql` for every one of them
whichever name the sentinel is reached by. The reachability runs from the module
to the kit package:
`modules/auth/contracts` reaches `kit/crud` through `modules/user/contracts`,
which imports it for `crud.Base` and not for a sentinel, and reaches `kit/db`
through four of its own files' service signatures. No kit package reaches a
module — that is a boundary the compiler and `./scripts/check_imports.sh` hold,
not a fact this guide states. Nothing here is a gate that now passes. The three
source contracts are still candidates — `Render`, `Agrees` and a package-level
derivation take no transaction, which is the shape this package exists for — and
moving them is those modules' change, not this one's; meanwhile the alias is why
their refusal is already this one object. The count above is measured rather than
remembered, and two cases refuse it going stale:
`TestTheGuideNamesEveryCallerThatStillWrapsTheAdapterName` reads the modules'
`contracts/` and `domain/` sources, and
`TestEveryContractsPackageThatWrapsTheSentinelIsNamedByAGuide` reads them one
directory deeper, where a module's test-support package lives — which is where
the eight above hid from the version of this paragraph that counted four. The
exported `var`s are reassignable, because the language cannot forbid it; the
package holds no state, so reassigning one is a bug in the caller. The consumers
this was written for are downstream of this repository — a consumer's
`contracts/` package whose service takes no transaction — and adopt it when their
pin moves. This guide does not name them: ADR 0009 keeps
a private catalog's capabilities out of a public document, and a name published
here is not withdrawable. That rule is checked rather than hoped for:
`review2_public_guide_test.go` in this package refuses a kit guide naming a
capability this tree does not hold, and `review3_public_documents_test.go` applies
the same rule to every markdown document the repository publishes, which is where
a release note or an ADR is read for the name it should not carry.
