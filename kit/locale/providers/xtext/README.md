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
