package personaldata

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// match is one replacement the scrub makes in one column: what to look for, and — for the
// one string that cannot be told from an ordinary word — which rows may be searched for it.
type match struct {
	// pattern is what every row of the scope is searched for.
	pattern string

	// ownPattern is what only a row carrying the person's own id is searched for, and it is
	// set only beside a handle: read as a bare word it is the person in their own rows and
	// somebody's verb in everybody else's. The row that does not carry the id keeps the
	// narrower pattern above, so the two live in one expression the server decides per row.
	ownPattern string
}

// matches expands what a person is known by into the replacements to make, longest
// identifier first: an address and a name as whole words wherever they sit, and a handle as
// the @-mention it always is, or — in a row that names the person by id — as the bare word
// too. See Subject.Known.
//
// For a jsonb column each identifier joins a second time, as the document's serialisation
// spells it: see jsonEscaped.
func matches(s Subject, isJSON bool) []match {
	ids := s.Known()
	if isJSON {
		// Appended rather than replacing, so both spellings are searched in one statement:
		// a document that arrived through a client that had already escaped once holds the
		// raw text inside the serialised one, and a scrub of a jsonb column that reached only
		// for the stored text would certify that row clean.
		escaped := make([]Identifier, 0, len(ids))
		for _, id := range ids {
			if e := jsonEscaped(id.Value); e != id.Value {
				escaped = append(escaped, Identifier{Value: e, Login: id.Login})
			}
		}
		ids = append(ids, escaped...)
	}
	out := make([]match, 0, len(ids))
	for _, id := range ids {
		if id.Login {
			out = append(out, match{pattern: mentionPattern(id.Value), ownPattern: boundedPattern(id.Value)})
			continue
		}
		out = append(out, match{pattern: boundedPattern(id.Value)})
	}
	return out
}

// jsonEscaped is how Postgres writes one string when it serialises a jsonb document, which
// is the text a scrub of a jsonb column searches: `payload::text` is not the string that
// went in, and a name containing a quote or a backslash is not the same bytes on both sides
// of that cast. `Jo "JJ" Jones` is stored as `{"displayName":"Jo \\"JJ\\" Jones"}` — in the
// text the scrub runs over, `Jo \"JJ\" Jones` — so the raw pattern matches nothing there and
// the person's whole name survives an erasure that reported success.
//
// It is written out rather than produced by encoding/json because the two serialisers do not
// agree, and the one that decides what is in the column is the server: Go escapes U+2028 and
// U+2029 and, by default, `<`, `>` and `&`, none of which jsonb escapes. A name with a `&`
// in it is not rare, and a pattern for a spelling the document never holds leaves the name
// where it was while the re-read asks the same pattern and answers that nothing is left.
func jsonEscaped(s string) string {
	var out strings.Builder
	out.Grow(len(s) + 8)
	for _, r := range s {
		switch r {
		case '"':
			out.WriteString(`\"`)
		case '\\':
			out.WriteString(`\\`)
		case '\b':
			out.WriteString(`\b`)
		case '\f':
			out.WriteString(`\f`)
		case '\n':
			out.WriteString(`\n`)
		case '\r':
			out.WriteString(`\r`)
		case '\t':
			out.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&out, `\u%04x`, r)
				continue
			}
			out.WriteRune(r)
		}
	}
	return out.String()
}

// boundedPattern quotes a Postgres ARE identifier and adds zero-width word
// boundaries at Unicode letter, digit or underscore ends. Punctuation ends stay
// unbounded. The database must use a compatible UTF-8 ctype for these boundaries;
// matching whole runes avoids rewriting longer names with the same accented prefix.
func boundedPattern(id string) string {
	first, _ := utf8.DecodeRuneInString(id)
	last, _ := utf8.DecodeLastRuneInString(id)
	var out strings.Builder
	out.Grow(len(id) + 16)
	if isWordRune(first) {
		out.WriteString(`\m`)
	}
	out.WriteString(escapePattern(id))
	if isWordRune(last) {
		out.WriteString(`\M`)
	}
	return out.String()
}

// mentionPattern matches an explicit @handle outside owner rows. A following
// dot and word character make it a mailhost, so that spelling must not match.
func mentionPattern(id string) string {
	out := "@" + escapePattern(id)
	if last, _ := utf8.DecodeLastRuneInString(id); isWordRune(last) {
		out += `\M(?!\.[[:alnum:]])`
	}
	return out
}

// isWordRune reports whether Postgres' ARE word boundary would count r as a word
// character on a database in a UTF-8 locale: `[:alnum:]` and the underscore, which is the
// same set this file can decide without a server, and which is why the check is a rune one
// — see boundedPattern.
func isWordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// escapePattern quotes an identifier for Postgres' ARE pattern syntax: every
// metacharacter it could mean gets a backslash, so a person's address — a legal place
// for a dot, a plus, a bracket and a question mark — matches itself and nothing else.
//
// It happens here and not in SQL because Postgres carries no regexp_quote to do it
// server-side (the first run of this file found that out with a refusal), and doing it
// in Go is the version that can be read and tested without a database. The token a
// scrub leaves behind carries no backslash and no digit, so the replacement side of the
// expression needs no such treatment.
func escapePattern(id string) string {
	var out strings.Builder
	out.Grow(len(id) + 8)
	for i := 0; i < len(id); i++ {
		switch id[i] {
		case '.', '\\', '+', '*', '?', '(', ')', '[', ']', '{', '}', '|', '^', '$':
			out.WriteByte('\\')
		}
		out.WriteByte(id[i])
	}
	return out.String()
}
