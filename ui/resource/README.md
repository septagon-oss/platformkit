# Resolved resource screens

`List`, `Detail` and `Form` render `kit/entity/present.Resolved` with supplied
rows and authorized route descriptions. `ui/screens` localizes declarations and
resolves the same policy as the portable catalogue after narrowing commands by
the caller's guards. Rendering performs no reads, writes or authorization.

## Composition

**Reused** — `present.Resolve`, `present.Title`, `present.Ago`, `Words`, portable
forms, `DataList`, `DetailList`, `Time`, native POST forms and guarded resources.
**Added** — a whole-policy input, exact-number projection, typed filter transport
and HTML/browser assertions because the former web reader selected presentation
independently of the shared resolver.
**Made reusable** — every generated resource shares the same policy reader and
the fixture-linked HTML proof, including deterministic clocks and timezones.

## Policy and authority

A supported `Resource.Resolved` block supplies all presentation decisions.
Explicitly empty membership, options, help and missing-value words remain empty.
An absent block or unknown version resolves the original declarations once;
individual missing fields never fall back to raw hints. Invalid supported
references return an HTML render error. Inputs are immutable for the render.

Resolved nouns are printed as the policy holds them. Where a noun begins a line
— a list's heading, its table region's name, a record's collection crumb — the
page supplies the letter that starts a sentence; inside a sentence, as in the
result count and the empty-state description, the word stays exactly as its
author wrote it. `ui/resource` owns that line-initial form;
`kit/entity/present` resolves the noun and re-spells nothing.

Columns, overview, declared sections and record information retain policy order.
Equal labels do not identify fields. Empty columns keep the successful count and
pagination without inventing a table; empty detail values retain their labels.
Forms use the resolved create/update layouts and controls. Command-owned fields
may follow writable update fields as unnamed read-only facts. Hidden revision
conditions remain transport values, including after a refused submission.

`Commands` contains only routes the adapter authorized. Policy-only commands have
no form; invisible/system commands have none either. Primary commands precede
ordinary commands and receive the filled treatment, with New/Edit secondary.
Destructive commands use danger tone. Confirmation copy is declared, never
inferred. Successful POSTs show explicit success copy and Continue; absent copy
keeps the no-result redirect. A query parameter cannot manufacture success.

Resolved sort query values travel unchanged. Repeated `filter=field:value`
parameters are closed enum/boolean equality choices; the adapter validates and
coerces them before the read. Unknown sorts/filters, invalid values and duplicate
filter fields return 422. Choice changes reset paging; pagination preserves all
selections. Route guards and the authoritative command still enforce access.

## Values and environment

Missing and null values use the policy's missing word before boolean formatting.
False, zero and unknown enum values remain distinct. REST row projection retains
JSON numbers exactly. Integer and money readings reject fractional, overflowing
or already-approximate unsafe values. Money requires the row's declared currency
and scale 0..3; no currency or precision is inferred.

`Options.Rendered` is the instant the request sampled once, so one page cannot
measure two values against two nows; its zero value keeps them absolute.
`Location` selects server timezone, with UTC as the offline fallback. Without an
explicit location, the browser applies its own timezone. Dates stay dates;
absolute instants stay absolute. Relative units truncate signed differences to
seconds/minutes/hours/days, including future values and ages beyond seven days.
Unavailable Intl preserves the server text. Unmarked legacy time nodes retain
their previous controller behavior.

Declared copy uses `hints.<module>/<entity>.<aspect>`. Aspects include singular,
plural, description, emptyDescription, group/section keys, field label/help,
enum values, command label/confirmation/successMessage and deck direction labels.
Command argument aspects live under `command.<verb>.field.<name>`. Missing
translations retain declared copy; machine names, predicates and values never
translate. `PolicyText` maps common resolver words to the screen catalogue.

`Words.Value`, `FieldLabel`, `FieldHelp` and `EnumWord` remain deprecated delegates
for compatibility. Use resolved fields and `Value` for full row/environment
formatting. Their removal requires a later public API migration.

## Verification and limits

[HTML parity](../screens/policy_html_test.go) renders each reviewed fixture that
has a [literal web companion](../screens/testdata/parity/web/expectations.json);
it names each screen it leaves unasserted and refuses a fixture in neither.
Companions pin source hashes and await independent review. Browser cases: [resolved-time-policy](../../e2e/resolved-time-policy.spec.ts).
Run `go test ./ui/resource ./ui/screens ./ui/forms ./ui/components ./kit/rest`.
These proofs do not certify mobile rendering, RLS or transactional command
invariants; those remain the existing capability suites and application journeys.
