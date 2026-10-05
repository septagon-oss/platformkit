# Translation module

`modules/translation` owns one table in which any declared field of any record
can be said in another of the languages a tenant is served in. It owns the
storage, the staleness rule and the machine-translation door; it owns no route,
no page and no permission, and a person never opens a screen in this module. The
record is the door — its route answers `?lang=`, and its permission guards every
translation write — which is why the module the translated field lives in is the
module a caller authenticates against.

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

Both doors are the record's.

| Door | What it does |
| --- | --- |
| `GET …?lang=pt-PT` | the record, with the locale's own rows overlaid and every fallback named in `_i18n` |
| `PATCH …?lang=pt-PT` | writes translation rows, validated by the field's own rules; the record itself is untouched |
| `POST …/review` | marks reviewed; refuses a field whose source has moved, because a badge nobody checked is a lie |
| `POST …/suggest` | saves a machine draft, `origin=machine`; refuses when no provider is configured |
| `DELETE …?lang=pt-PT&field=…` | removes the locale's rows for one field |

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

## Composition

```go
translationSvc, translationModule := translation.Module(translation.Deps{
    Sources: []rest.TranslationSource{rest.TranslationSourceOf(contentSpec)},
})
contentSpec.Translations = translationSvc
```

**Reused** — the delivery is composed from `kit/richtext.SourceHash` (the digest
staleness is decided by, uncalled until now), `kit/crud`'s `GetForUpdate`,
`Classify` and `ErrConflict`/`ErrInvalid`, `kit/events`' `Declare`/`Publish` and
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
- Nothing reads the port from `kit/rest` yet. `Spec.Translations`, the `?lang=`
  write and `Base.I18N`'s population land with it; until then no route answers
  in a second language, and the module is composed by nothing — which
  `scripts/bespoke.py` counts as uncomposed rather than bespoke.
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
