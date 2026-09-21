# Page localization

The composing application supplies `Shell.Messages`, PlatformKit's `page.Messages`
contract. `page.FromCatalog` adapts an x/text catalog built before serving requests.
Modules own namespaced message keys and authored translations; applications
compose them into a catalog for their product.
Do not use `message.SetString` or change `message.DefaultCatalog`: those mutate
process-wide state and can mix different applications' copy.

`Serve` supplies a request-local `Request.Locale` with its supported content
language and the owned `page.Formatter` contract. `Shell.Locale` may resolve
an explicit URL, account or tenant preference. Unsupported or malformed preferences fall through
to the browser's weighted `Accept-Language`, then the catalog's configured default.
Preference storage and locale-preserving links belong to the application. A
language preference never selects a tenant or grants access.

Start with `catalog.NewBuilder(catalog.Fallback(language.English))` and add the
module's messages using `SetString` or `Set`. For example, these calls define an
action and its count without another interpolation or plural engine:

```go
if err := messages.SetString(language.English, "task.complete", "Complete task"); err != nil {
	return err
}
if err := messages.SetString(language.EuropeanPortuguese, "task.complete", "Concluir tarefa"); err != nil {
	return err
}
if err := messages.Set(language.English, "task.open",
	plural.Selectf(1, "%d", plural.One, "%d open task", plural.Other, "%d open tasks")); err != nil {
	return err
}
if err := messages.Set(language.EuropeanPortuguese, "task.open",
	plural.Selectf(1, "%d", plural.One, "%d tarefa aberta", plural.Other, "%d tarefas abertas")); err != nil {
	return err
}
```

Assign `page.FromCatalog(messages)` to `Shell.Messages` after composing the catalog.
In a localized handler, `r.Locale.Text("task.complete", "Complete task")`
produces the action label; `r.Locale.Text("task.open", "%d open tasks", count)`
formats the count. Pass resulting strings through `g.Text` or typed component
labels. Translations and interpolated user input are text, never trusted HTML.
Retain operation names, permission keys, enum values and API fields unchanged.

The catalog selects CLDR plural forms and parent-language translations through
x/text. Supply readable source text with every `Text` call; a missing message
uses that source text. The configured catalog fallback chooses the default
language, not missing entries in unrelated languages. Review catalog completeness
before claiming a fully translated screen, and mark deliberately mixed-language
copy appropriately.

An empty `View.Language` uses the selected locale. An explicit view language still
wins, and the frame receives that language's formatter. A handler rendering an
explicit language must select it before formatting its body; `SelectLocale` also
works for that case and for non-HTTP consumers. Language metadata cannot translate
already rendered text. Negotiated pages emit `Content-Language`, `Vary:
Accept-Language` and `Cache-Control: private, no-store` so an account preference
cannot leak through a shared response cache. Existing unconfigured shells retain
their behavior; untranslated recovery notices keep their English tags. A refusal
page (`FaultHandler`, the page a kernel guard answers a browser with) reads the
same contract: it negotiates from the request's `Accept-Language` — a guard
answers before a tenant, a session or a stored preference exists, so the header
the caller brought is all there is to negotiate from — and shows the sentence this
shell ships under `fault.<CODE>`, where the codes are `kit/httpx`'s published
`Code*` constants and the table lives in `fault.go`. A refusal with no code is
keyed by its status (`fault.404`, `fault.500`). The kernel's English text is the
fallback, and the page declares the language of the sentence it actually shows:
a catalog with no entry for the code leaves an English page declaring `en`.

The [reference application](../../apps/platformkit/modules.go) composes
[`admin.Messages()`](../../modules/admin/messages.go) through `page.FromCatalog`
for its sign-in page. Run the application as described in the [root README](../../README.md), then open
`/app/admin/login?lang=pt-PT` or `/app/admin/login?lang=en`. The explicit URL wins over the
browser's language. This translates the initial sign-in form, and the generated
screens read their fixed labels (New, Edit, Delete, the count, the pager and the
empty state) under `screens.*` keys with English fallbacks; entity names,
authentication API errors, client apps, the kernel's refusal sentences
(`fault.*`) and notification templates still need their own authored messages and
adoption. No translation management service or remote bundle is required by this
local runtime seam.

Run `go test -race ./ui/page -count=1` for negotiation, fallbacks, pluralization,
escaping and concurrent catalog isolation. With the repository's existing test
database configured, this exercises the composed HTTP sign-in page and headers:

```sh
go test ./apps/platformkit -run '^TestReferenceSignInUsesIsolatedNegotiatedTranslations$' -count=1
```
