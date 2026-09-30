# x/text catalog provider

`FromCatalog(catalog.Catalog)` implements the shared
[locale contract](../../README.md) using Go's x/text language matching, CLDR
number formatting, interpolation and plural rules. It adds only x/text
dependencies; it imports no web, UI or database packages.

Build an explicit catalog with its fallback and namespaced messages, then pass
it to `FromCatalog`. A nil or empty catalog panics. The provider retains the
catalog, so finish building it before rendering and do not modify it during
concurrent use. Each selection creates its own formatter; no global message
catalog is read or written.

## Catalogues from files

`Load(fallback, sources...)` reads gotext JSON catalogues — one `<locale>.json`
per locale, in each owner's own directory — and merges them in argument order,
so a product can re-word a module's sentence and a client the product's, without
either editing the other's file. A source may `Own` key prefixes, and a later
source that answers for an owned key is refused at boot: the sentence a kernel
refusal speaks stays the kernel's.

The `fallback` names the language the code is written in, so that file is the one
locale a catalogue may omit — a key with no entry in it answers from the readable
text its call site passes, which is the same promise `Text(key, fallback)` makes.
Every other locale must answer for every key the source wrote a copy for, in both
directions, and no copy may change the arguments the sentence interpolates. The two
conditions are read at different scopes on purpose: presence is each source's own,
since a layer that ships one locale and a product that re-words one key are what the
merge exists for, while the arguments are the merged catalogue's, because a re-wording
merged over a sentence is read by every language an earlier source wrote — the
argument lists of one key across all the sources are refused as one. Both are refused
with a message naming the file, the source that wrote it and the key, because a
catalogue nobody can answer from must fail a boot and not a person.

Plural messages stay composable by hand through `FromCatalog`; `Load` refuses a
`key#one` style key rather than build a selector nobody asked it for.

The [file example](load_test.go) composes a module's two files and selects
Portuguese from a worker. The parity refusals, the merge order and the ownership
rule are each tested where they are enforced.

The [worker example](catalog_test.go) selects Portuguese from a recipient
preference and renders text without starting a server. Ordered preferences may
include browser language lists when supplied by a web adapter. The selected
`Language` names supported content; number formatting can retain a more specific
regional preference. Missing messages use the supplied readable fallback.

From the foundation repository, run `go test -race ./kit/locale/...` for the
contract and provider cases. `go test ./ui/page -run 'TestLocale|TestLocales'`
checks existing page negotiation, plural, escaping and catalog-isolation behavior
through the compatibility adapter. Page/request header integration remains with
the foundation's full verification workflow.

### Built on what came before

Decision 0022 asks a delivery to name what it composed rather than what it
rebuilt. **Reused:** `locale.Messages`, the contract `FromCatalog` returns, and the `locale.Formatter` behind it —
what `Load` hands back satisfies that same contract and adds one readable method, so every
existing caller, every plural rule and every number format is untouched by where its copy
came from, and `language.Parse` is what decides
a file's name is a tag; the source-language promise is the one `Text(key, fallback)` already makes, which is why the fallback
needs no file of its own. **Added:** the merge in argument order, the `Owns` guard,
and the refusals a source's files can meet — a locale file missing a key its own
source answers for, a blank copy, a copy that changed the arguments its other copies
interpolate (re-ordering them is legal and is what `%[2]s` is for: what is refused is a
copy asking for a different *set* of arguments, not one reading them in another order), an
argument list two sources left disagreeing about one key, an owned key answered by a
later source, an unreadable name — none of which existed to
be composed before this seam. **Made reusable:** the one call a composition makes, so
a module, a product and a client each ship their own `messages/` directory and are
read in the order somebody wrote down, with no registry, no environment variable and
no process-global catalogue to be discovered by.

The number this seam is judged on is not "translations exist" but *coverage of the copy the
kernel itself raises*: the keys its refusals and generated screens can ask for, and how many of
them a second language answers for. Count them with the two coverage gates — raise the set in
`ui/page` and `ui/resource`, list the keys the shipped `messages/` files answer for — and refuse
the two lists to differ. Four of the kernel's twenty-nine keys were answerable in European
Portuguese before this seam existed and twenty-nine are now; a count that goes stale fails
`TestTheCatalogueAnswersEveryRefusalThisPackageCanShow` rather than sitting in a README.
