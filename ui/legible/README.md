# legible — what a person reads in a served document

`Scan(body, reached)` parses one response body and hands back every string a person
could read in it: each text node, and each `alt`, `title`, `aria-label` and
`placeholder`, with the path it sits at (`html[1] > body[2] > main[1] > div[1]#text`,
element siblings numbered). `Violations` are the strings that went around a catalogue;
`Exempted` are the ones the number does not count. `Exempt(text)` is the exemption
rule, and `Reached` is the decision handed in by whoever marks the copy —
`pseudo.Wrapped` in the gate — so this package names no locale and no delimiter.
`MarkDatum(collected, values)` marks the strings the application itself holds as what a
person typed, and `Report(label, collected)` prints the verdict one document at a time:
`TEXT <label> <path> "text"` for every string that went around a catalogue and
`DATA <label> <path> "text" <why>` for every string it declined to count, with the rule
that declined.

It reads documents and writes nothing: no I/O, no clock, no tenant, so a case asserts
the whole list against a literal.

The two decisions worth arguing about are both refusals to be convenient. Nothing is
skipped but `script`, `style`, `template` and `noscript`: `<title>` is copy a person
reads in a tab, and `aria-hidden="true"` stays in the count, because hiding a node
from assistive technology does not make an English label translatable and honouring it
would hand out a switch for this gate. And `Exempt` is a function of what a string
*is* — no letters, or nothing but machine shapes (numerals, ids, timestamps, amounts
with a Currency, opaque tokens, keys, language tags, hosts, file names) — never of
where it sits, because a predicate satisfiable by position is one somebody would
satisfy by position. A mixed line ("Delete task 4b2a…") is therefore reported: the
word that is not a shape is a sentence somebody wrote in Go. Dates exempt only in the
numeric forms this kernel renders; a page that spells `Monday, 4 October 2026` is a
page whose month names nobody translated, and that is reported. An address exempts by
shape — a path from the root, a media type, a product tag and its version — and not by
holding a slash, because `Yes/No` and `Show/Hide` hold one and are copy. What no rule
over a shape can decide is which sentences a person typed: "Pump room inspection" and
"Back to the workspace" are the same kind of text, and only the write that stored the
first knows it, which is why the gate hands its own seeded values to `MarkDatum`.

**Reused** — `golang.org/x/net/html` (`html.Parse`, already a requirement) rather than
a second HTML reader, and the raw-text skip that falls out of the parse tree: a
`<script>` body is a text node whose parent names it. **Added** — the scan and the
exemption grammar; nothing in the tree read a rendered document back (`grep -rln
"html.Parse"` returned two test files) and there was no exemption rule to extend.
**Made reusable** — the scan, `Exempt`, `MarkDatum` and `Report` are one client's own
gate's halves: a product renders its pages under the same pseudo-locale, calls `Scan` with
`pseudo.Wrapped`, marks what it typed, and floors its own ratio, without this package
knowing what a page, a route or a client is.

Known over-reach, printed rather than hidden: `of-PT` passes as a language tag, a
16-character slug holding a digit passes as an opaque token, and `and/or` reads as a
relative address because every one of its segments is spelled the way an address
segment is. `Report` prints every exempt string with the reason, so what the number
declined to count stays reviewable.
