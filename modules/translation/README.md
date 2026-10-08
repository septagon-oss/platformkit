# Translation module

`modules/translation` owns one table in which any declared field of any record
can be said in another of the languages a tenant is served in. It owns the
storage, the staleness rule and the machine-translation door; it owns no route,
no page and no permission, and a person never opens a screen in this module. The
record is the door: the route that reads a page is the route that will answer
`?lang=`, and that route's permission is what guards every translation write —
which is why the module the translated field lives in is the module a caller
authenticates against. Those doors are the slice that composes this module, and
[Reads, writes, refusals](#reads-writes-refusals) says plainly which commands run
today and which addresses do not exist yet.

Compose it with `translation.Deps{Sources: …}` and hand the returned service to
every `rest.Spec` that declares a translatable field. Consumers import
[contracts/](contracts/) and its [fake](contracts/translationtest/), never
`internal/`.

## What one row is

A row is **one field of one record in one language** — not one record, and not
one record in all languages. Identity is
`(tenant_id, module, entity, record_id, field, locale)`, and everything a
translator is shown is derived from those rows plus the current source:

| State | It means |
| --- | --- |
| `missing` | no row exists for that field and locale |
| `complete` | a row exists and its `source_hash` still matches the source |
| `outdated` | a row exists and the source has moved under it |
| `machine` | a machine draft nobody has reviewed |

`missing` is a state of a pair with no row, which is why the column holds three
values and a response can name four. A translation has no lifecycle of its own
and no soft delete: it is written by a write of the record, deleted by a delete
of the record (`ForgetRecord`, in the same transaction), and there is nothing
else.

## The source copy, and why it is stored

`source_text` and `source_hash` are the source as the translator saw it, and
both are written by `Save` inside the source row's own `FOR UPDATE`, so they
describe a source that existed at commit and not one that existed when the form
was opened.

They are not recomputed when the source moves again. The stale pair *is* the
reviewer's evidence: re-basing `source_text` onto the newest English would
delete the paragraph the reviewer was told to look at, which is the one thing
the side-by-side view exists to show. `translations/internal/diff.go` therefore
differs the stored copy against the current source, paragraph by paragraph, and
says which paragraphs of both sides to highlight. `SourceHash`
(`kit/richtext`) is the digest for a richtext field; everything else is
normalised by `NormalisePlain` — LF newlines, no trailing spaces, no run of more
than one blank line, and never any interpretation of markup, because a title
containing an asterisk is a title containing an asterisk.

The one bound: past 2,000 paragraphs on either side the paragraph claim is
refused and the answer is `Diff.All` — "the whole field is outdated, no
paragraph-level claim" — because a quarter-megabyte body can hold enough
paragraphs to make an O(n·m) diff cost a page of an overview seconds.

## What is never served

Nothing a machine wrote reaches a public reader until a person marks it
reviewed. The rule is one line inside `Service.Translated`, keyed on
`reviewed_at IS NULL` and not on `origin`, because a person who typed over a
machine draft is reviewed by having typed — and `origin` stays `machine`, since
provenance is never erased. The workspace sees the draft, labelled; the public
door sees the source and a `_i18n` status of `withheld`.

A provider that hands back the text it was given saves nothing. That is the
failure mode of a machine translator that saves a draft: English filed as
Portuguese, which nothing downstream would ever notice.

## Reads, writes, refusals

These are the port's commands. Both doors exist in `kit/rest`; the module is
composed by nothing yet, which is what Limits says plainly.

| Door | What it does |
| --- | --- |
| `GET …?lang=pt-PT` | for a Spec whose entity declares a translatable field and whose `Translations` port is wired: the locale's rows overlaid, every field that fell back named in `_i18n`, and `Content-Language` set. A resource with no translatable field does not offer `?lang=` at all, and its JSON is what it always was |
| `POST …/translate` | writes this language's text of the named fields, as a person's own typing. The body carries each field's expected revision, so two translators in one locale have one loser and the six fields of a save are refused together. The record's own columns are untouched: this command cannot move the English |
| `POST …/review-translation` | marks the named fields reviewed, or every field of the record when the body names none |
| `POST …/suggest-translation` | asks the machine for a draft of the named fields and saves it unreviewed; refuses, writing nothing, where the installation names no translator |
| `POST …/untranslate` | removes this language's rows for the named fields, or for all of them |

Every one of the four is a command **of the record**, mounted by the same
`Spec.Mount` that mounts its `PATCH`, guarded by that Spec's own `Write`
permission, and refused before it touches anything if the language is one the
tenant is never served in or is the tenant's own. The text is validated by the
field's own rules — the same `richtext.Prepare`, ceiling and `Files` port the
record's own write runs — so a translated body that would be refused in English
is 422 in Portuguese, with the same remedy in the problem document.

The record's `PATCH` keeps one meaning: it writes the record. Translating it is
a different act, with a different language in its body and a different
staleness rule, and an address whose meaning depends on a query parameter is an
address a caller cannot reason about and a shell cannot describe.

What `kit/rest` does beside the two doors: `Spec.Translations` (whose absence
`check()` refuses for any entity declaring a translatable field), `Spec.deleteRow`
→ `ForgetRecord`, and a source `PATCH` that moved a translatable field →
`MarkOutdated`.

| Command | What it does |
| --- | --- |
| `Translated` | the locale's rows for a page of records, each with the fallback a reader must be told; `Public: true` withholds an unreviewed machine draft |
| `Save` | writes the fields the caller named — every field of the record when none is named — with the text, the expected revision and the source the door handed over; the field's own rules ran at the door above, not here |
| `Review` | marks reviewed; refuses a field whose source has moved, and refuses one whose source was never handed over, because a badge nobody checked is a lie |
| `Suggest` | saves a machine draft, `origin=machine`; refuses when no provider is configured |
| `Untranslate` | removes the locale's rows for the named fields, or all of them when none is named |
| `Overview` | one entity and locale over a page of the entity's own rows, filtered by derived state when one is asked for |

Every refusal writes nothing, publishes nothing and returns no stale row. A
stale `expected revision` is `crud.ErrConflict` — two translators in one locale,
one loser, no silent overwrite — and the check runs over every field *before*
any field is written, so a save that loses on one of six writes none of them.
A refused save is asserted by a conformance case that reads the table and the
outbox afterwards.

`translation.updated` is the module's only event, published in the caller's
transaction beside the row it describes. There is no `audit.Record` call in this
module and there must never be one: `modules/audit` subscribes to every declared
event, so emitting this is what puts a row in the trail — actor, request id,
traceparent — and writing the trail here as well would be a second account of
what already happened.

## Authorization

Decision 0011's six questions, answered for a module that declares no permission
and mounts no route (`Permissions: nil`, `Routes: nil` in the manifest). The
record is the door, so the record's grants are the whole authorization story.

### Permissions

None. The module declares no permission key and would refuse one: a key here
would be a second door to a row the record's `Spec` already guards, and a caller
holding it could translate a record they cannot read. What guards a translation
is the mounting Spec's own `Read` for `?lang=` and `Overview`'s callers, and its
`Write` for `POST …/translate`, `…/review-translation`, `…/suggest-translation`
and `…/untranslate`, which is what those four commands are mounted behind
(`kit/rest/translations_write.go`). Whether an account is a translator rather
than an editor is the product's share, asked of the same grant.

### Object scope

Rows are keyed `(tenant_id, module, entity, record_id, field, locale)`, and the
tenancy half is row-level security — `platformkit_tenant_match`, `ENABLE` and
`FORCE`, so a `SELECT` that names no tenant answers zero rows rather than
somebody else's page. Per-record scope is the door's: the write commands lock the
source row with `crud.GetForUpdate` and call `crud.RecheckTenant` before the
port sees a field, which is where a right-tenant, wrong-record caller is
refused. This module does not re-ask whether the caller may see a given record;
it has no route on which to ask, and a port that guessed would be a port that
could read a row the door had refused.

### Duties the module enforces itself

Four, each inside the authoritative transaction, each refusing with nothing
written and nothing published:

- an unreviewed machine draft never reaches a public read (`Public: true`);
- a `Review` whose source has moved, or was never handed over, stamps nothing;
- a stale `expected revision` is `crud.ErrConflict` over every field before any
  field is written — a save that loses on one of six writes none of them;
- a machine that returns the text it was given saves nothing, because filing
  English as Portuguese is a draft no reader would ever notice.

### Public faces

`GET …?lang=pt-PT` on a Spec with a translatable field: the locale's rows, every
fallback named in `_i18n`, `Content-Language` the tag asked for. Withheld from a
public reader: anything a machine wrote that no person has reviewed — the source
and a `withheld` status answer instead. No page, no screen, no admin surface:
the translator's own screens are the next slice and are not this module's.

### The operator boundary

The machine translator's address, key and price are an operator's facts and live
in the installation's configuration (`translation.Deps.Translator`), never in a
tenant's data or a caller's request: a tenant cannot point the module at a
provider, and where none is configured `Suggest` refuses and writes nothing.
Which languages exist at all is the installation's catalogue; which of them a
tenant speaks is `modules/tenant`'s answer to its own operator.

### Provisioning

Nothing. No rows to seed, no default to write: a tenant's languages come from
`modules/tenant`, and a translation row appears only when somebody writes one.
Adding the module to an installation adds a table and an event to the deployment
and changes what no existing tenant sees until a field of theirs is tagged.

## Composition

```go
translationSvc, translationModule := translation.Module(translation.Deps{
    Sources: []rest.TranslationSource{rest.TranslationSourceOf(contentSpec)},
})
contentSpec.Translations = translationSvc
```

**Reused** — the delivery is composed from `kit/richtext.SourceHash` (the digest
staleness is decided by, uncalled until now), `kit/crud`'s `Classify` and
`ErrConflict`/`ErrInvalid` (the module locks its own rows with `clause.Locking`,
not with `crud.GetForUpdate`, because a translation row is not a `crud.Base`
entity), `kit/events`' `Declare`/`Publish` and
the outbox's trace baggage, `modules/audit`'s `SubscribeAll` (which is the whole
audit trail, so this module writes no audit call), `kit/db`'s tenant-scoped
`Tx[db.Tenant]` under the `platformkit_tenant_match` RLS policy, and the
`modules/audit/contracts/audittest` pattern for the conformance fake.

**Added** — the `translations` table, because no merged table can key a value by
another module's row id plus a field name plus a locale; the `rest.Translations`
port and `entity.Base.I18N`, because nothing in the response vocabulary could
carry a per-field fact about which language a value is written in;
`i18n:"translatable"` on `entity.Field`, because the declaration has to reach
`ui/screens` and therefore has to live in the schema; and the paragraph diff,
because a diff library is not a dependency and 0069 §1 refuses a regular
expression over canonical Markdown — a table and a fenced block both contain
blank lines that are not paragraph breaks.

**Made reusable** — `translationtest.RunService` is the port's specification as
executable cases, run by the fake and by the SQL service alike, so the next
implementation of a module-port has a second one to copy; `rest.TranslationSourceOf`
is a general mechanism for "a module may ask another module's rows about a
question it cannot answer from its own table" without importing that module; and
`kit/richtext.Paragraphs` is the canonical block list any paragraph-aligned
feature (compare, diff, chunk for search) now has one owner for.

## Limits

- Nothing serves the translator's screens yet: the switcher, the side-by-side
  view and the overview are the next slice, and `Overview` exists for them.
- `?lang=` is honoured only when it is asked for. There is no negotiation: no
  cookie, no `Accept-Language`, no `Vary`, and no preference stored — the tenant's
  declared list decides whether a tag is answered at all, and a read in the
  tenant's default language asks nothing of this table.
- Still missing: the public `lang`/`hreflang`/`x-default` set, the translator's
  UI (switcher, side-by-side, overview), the machine adapter behind
  `locale.Translator` — no provider's URL, model or price lives in this
  repository, so nothing here pretends to be one — the Playwright+axe journeys
  and the second demo language in `config.example.yaml`. Above all, **nothing
  composes this module yet**: `grep -rl modules/translation apps` answers
  nothing, so no tenant is served a translated record today and every door above
  is a door no installation mounts until somebody writes the two lines in
  `apps/platformkit/modules.go`. Naming that here is the point: a README that
  described doors as present while nothing wired them is the finding this module
  was reviewed for.
- Composing it is a slice of its own rather than two lines, because three
  published facts move with it and none of them moves here. A Spec whose entity
  declares a translatable field answers `GET` as `TranslatedPageContentBody`
  where the reference application's pinned document publishes `PageContentBody`
  — the wrapper exists to carry `Content-Language`, and
  `TestTheOpenAPIDocumentIsTheCompositionServed` refuses a renamed response
  schema as a breaking change whatever `UPDATE_GOLDEN` says, so the door's body
  has to keep the page's name before any entity is tagged. `translation.updated`
  has to reach `testdata/asyncapi.json` as a channel, which
  `TestEveryDeclaredEventIsInTheDocumentAndCovered` counts. And this file gains
  what the section above already answers, read by
  `TestEveryComposedModuleREADMEAnswersTheSixAuthorizationQuestions` for every
  module the application names. Tagging content's `title` and `body` and wiring
  both lines at `9d09d98` failed those three cases, in that order, while
  `modules/content`'s own suite stayed green beside them once its `Deps` is
  handed the fake port — which is why the tagging, the wiring and the three
  documents belong in one later commit and not in a hurried one.
- The tenant's declared languages are a parameter of the caller, never stored
  here: which languages a tenant speaks is `modules/tenant`'s answer, reached by
  `SetLocale`.
- Whether an account is a translator rather than an editor is the product's
  share. The module keeps the mounting Spec's own `Write`.
- A machine provider is configured by an operator and priced by the product: no
  number for it lives in this repository, and none is quoted in UI copy.
- The `record_id` no foreign key can name is held by `ForgetRecord` and by case
  C16 alone. That is the honest cost of a cross-module reference, and it is the
  same cost every other module already pays.
