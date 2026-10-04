# pseudo — the locale that marks what a catalogue reached

`pseudo.Wrap(messages, recorder)` answers every selection as `en-XA`: the delegate's
own copy, wrapped in `⟦…⟧` with its vowels accented. The transformation is reversible
(`Unmark`) and length-preserving, so a document can be measured by machine and read
back by a person, and nothing about it moves a layout.

That is the whole point. Most `Text()` call sites build their key at run time, so no
source scan can say what a page says in which language; a rendered page can, because
only a formatter's answer carries the mark. `kit/locale` owns the contract
(`Messages`, `Formatter`, `Locale`) and `xtext` the copy; this is a second provider of
the same contract, and a test double: it is composed in
`apps/platformkit`'s test files and nowhere in any binary. `Wrap` returns
`locale.Messages`, not an `xtext.Catalog`, so `en-XA` joins no `Languages()` set, no
tenant's supported set and no `?lang=` choice — a person is never served in it. The
recorder attributes each ask to one page, `Begin` refusing a second open page rather
than mislabelling a sentence, which is how the gate says which keys no catalogue
answers.

**Reused** — `kit/locale.Messages`/`Formatter`/`Locale` and `locale.SelectLocale`'s
composition-time refusal, `xtext.Load` and `xtext.Catalog` as the delegate (including
its `Carries`, which is how an ask is known to have been answered rather than echoed),
`golang.org/x/text/language` for the tag, and the double-provider shape
`ui/page/tenant_locale_test.go` already built catalogues with. **Added** — the mark
itself and the ask recorder: no formatter wrapper existed in the tree (`grep -rn
"locale.Formatter"` returns the one alias in `ui/page/locale.go`), and
`golang.org/x/text` ships no pseudolocales to import, so the reversible wrap, the
vowel rotation that stays a bijection over any payload — including copy whose own
letters are accented, as Vietnamese and Portuguese write them — and the serial
page-attribution rule are this package's. **Made reusable** — the recorder's worklist
(`Unanswered`, per key, per page, per language) is any client's own translation
backlog, and `Carries` on `xtext.Catalog` answers "is this key translated?" for
whatever asks it, not only this gate's.

Refusals: a nil delegate panics at composition (`Wrap`), as does a delegate returning
an incomplete locale. A catalogue file named `en-XA.json` is refused by `xtext.Load`
itself — the tag belongs to this provider and no source may ship a language a
deployment would answer people in.
