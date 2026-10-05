package legible

import (
	"regexp"
	"strings"
	"unicode"
)

// separators are the glyphs a page puts *between* things — a breadcrumb's divider, a
// list's bullet, a metadata row's bar. A string that is only one of them, or one
// with words before it, is typography and not copy, so a trailing one is dropped
// before anything is decided. One between two words belongs to the line and stays.
const separators = "·|–—›»,/"

// canon reduces a string to what the exemption rule reads. The parser already
// resolved character references, so what is left is to fold the whitespace the HTML
// source put in and to drop a trailing separator. Case is kept on purpose: the
// machine grammars below read `pt-PT` and `Pt-pt` differently, which is what stops a
// hyphenated English word passing as a language tag.
func canon(text string) string {
	var folded strings.Builder
	space := false
	for _, r := range text {
		if unicode.IsSpace(r) {
			space = true
			continue
		}
		if space && folded.Len() > 0 {
			folded.WriteByte(' ')
		}
		space = false
		folded.WriteRune(r)
	}
	out := folded.String()
	for {
		runes := []rune(out)
		if len(runes) == 0 {
			return ""
		}
		if !strings.ContainsRune(separators, runes[len(runes)-1]) {
			return out
		}
		out = strings.TrimRight(string(runes[:len(runes)-1]), " ")
	}
}

// Exempt says whether a string is data rather than copy: something a person reads
// off a page that no translator was ever asked for. It is a pure function of the
// string, never of where the string sits or which element carries it, because a
// predicate that could be satisfied by position is one somebody would satisfy by
// position.
//
// Three things exempt: nothing there at all; no letters at all — a count, an arrow,
// a glyph, every `%d` a formatter interpolated; and a string made entirely of the
// machine shapes in §tokenGrammars. A mixed string ("Delete task 4b2a9c1d-…") does
// not exempt, because the word that is not a shape is the sentence somebody wrote in
// Go, which is exactly what this reports.
//
// A shape exempts only when the whole string has that shape and nothing else. Words
// a sentence puts around a shape — the `Name:` before a field, the `Sat 1 Jan` a
// page spelled out — are copy, and §separators and §wholeGrammars are written so that
// punctuation and words at the edge of a token are read as English, not as a key.
//
// A hyphen never exempts on its own: `Hard-coded`, `Sign-in` and `Follow-up` all
// count as copy. The residual risk is stated where it can be read — `of-PT` passes as
// a language tag, `onboarding-2026-checklist` as an opaque token and `and/or` as a
// relative address — and it is bounded by the report printing every exempt string,
// so nothing the number chose not to count is hidden.
func Exempt(text string) bool {
	text = canon(text)
	if text == "" {
		return true
	}
	if strings.IndexFunc(text, unicode.IsLetter) < 0 {
		return true
	}
	for _, whole := range wholeGrammars {
		if whole.MatchString(text) {
			return true
		}
	}
	for _, token := range strings.Split(text, " ") {
		if !machine(token) {
			return false
		}
	}
	return true
}

// machine says whether one token is one machine shape and nothing else.
func machine(token string) bool {
	if isMachineKey(token) || isOpaqueToken(token) {
		return true
	}
	if address(token) {
		return true
	}
	for _, grammar := range tokenGrammars {
		if grammar.MatchString(token) {
			return true
		}
	}
	return false
}

// address says whether one token is an address rather than a word: a path from the
// root or from here, or a pair of machine-shaped segments joined by a slash. It is
// not "anything with a slash", which is the mistake this rule used to make: a
// toggle's label, a column that reads `N/A` and a choice between two words are all
// spelled with a slash and all copy somebody wrote in Go.
func address(token string) bool {
	return absolute.MatchString(token) || relative.MatchString(token) || client.MatchString(token)
}

// The three address shapes. `absolute` is a path; `client` is a product tag and its
// version, which is the shape a stored User-Agent takes and the one data shape
// allowed its capitals — a product spells itself however it likes.
//
// `relative` is the one that costs something: every segment of `text/plain` and
// `uploads/2026/chiller.pdf` is lowercase ASCII with an address's punctuation, and
// so is `and/or`. The condition that makes a media type a media type is a condition
// on spelling, and a lowercase pair joined by a slash satisfies it. That is the
// over-reach this rule accepts, and `Report` prints it rather than hiding it; what
// it replaces is the version that exempted `Yes/No` on its way to exempting `text/plain`.
var (
	absolute = regexp.MustCompile(`^(?:\.\.?|~)?/(?:[A-Za-z0-9._~%+@-]+/?)*$`)
	relative = regexp.MustCompile(`^[a-z0-9][a-z0-9._+-]*(?:/[a-z0-9._+-]+)+$`)
	client   = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9.+-]*/\d+(?:\.\d+)*$`)
)

// The shapes the grammars below spell, written once.
const (
	// currency is a rendered amount's unit. House rule 10: money is int64 minor
	// units with a Currency, and a Currency is data, never a sentence.
	currency = `(?:€|£|\$|R\$|BRL|USD|EUR|GBP|INR|JPY)`
	// size is a byte count's unit.
	size = `(?:[KMGTPE]i?B|B|kB|MB|GB|TB|PB)`
	// numeral is a number as a locale would render it, with either separator.
	numeral = `[-+−]?\d{1,3}(?:[ ,.]\d{3})*(?:,\d+|\.\d+)?`
	// machineKeyPattern is a slug of the shapes a key takes; the condition that makes
	// it a key and not a word is isMachineKey's, not this pattern's.
	machineKeyPattern = `[A-Za-z0-9][A-Za-z0-9.:/+\-_]*`
	// opaquePattern is a token too long to be a word; whether it is a token or a slug
	// isOpaqueToken's condition answers.
	opaquePattern = `[0-9A-Za-z_-]{16,}`
	// emailPattern is one address, which is a person's data wherever it appears.
	emailPattern = `^[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}$`
	// uuidPattern and ulidPattern are row identity.
	uuidPattern = `[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`
	ulidPattern = `[0-9A-HJKMNP-TV-Z]{26}`
	// languageTagPattern is a BCP-47 tag: the region subtag, when present, is written
	// upper case, which is what makes `pt-PT` a tag and `Hard-coded` a word.
	languageTagPattern = `^[a-zA-Z]{2,3}(?:-[A-Za-z]{4})?(?:-[A-Z]{2}|-[A-Z0-9]{3})$`
	// hostPattern is an all-lowercase dotted host: `acme.localhost`.
	hostPattern = `^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]*[a-z0-9])?)+$`
	// filePattern is an uploaded file's name.
	filePattern = `^[a-z0-9][a-z0-9._-]{0,79}\.[A-Za-z]{2,5}$`
	// urlPattern is an address with its scheme written out: a link shown as text is a
	// location a person can paste, and the scheme is what says so. `Name:` holds no
	// scheme and no `://`, so this shape never reads a label as an address.
	urlPattern = `^[A-Za-z][A-Za-z0-9.+-]*://[A-Za-z0-9][A-Za-z0-9._~%!$&'()*+,;=:@/-]*$`
)

// wholeGrammars match an amount, an instant or a rate that spans spaces — a money
// cell, a timestamp, a percentage. They are tried against the whole string because
// splitting "R$ 1.234,50" on its space would ask whether "R$" is a word.
//
// Dates are exempt in the numeric forms this kernel renders, and only those: a page
// that spells "Monday, 4 October 2026" — or "Sat 1 Jan 09:00", or any of the eleven
// other English words a weekday and a month can be — is a page whose month names
// nobody translated, which is a decision about that page and not something this rule
// hides. No grammar below matches a word, so no spelling of a month exempts.
var wholeGrammars = []*regexp.Regexp{
	regexp.MustCompile(`^` + numeral + `$`),
	regexp.MustCompile(`^[-+−]?\d+(?:[.,]\d+)? ?%$`),
	regexp.MustCompile(`^[-+−]?(?:` + currency + `) ?[-+−]?[\d .,]+$`),
	regexp.MustCompile(`^[-+−]?[\d .,]+ ?(?:` + currency + `)$`),
	regexp.MustCompile(`^[\d .,]+ ?` + size + `$`),
	regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`),
	regexp.MustCompile(`^\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}(?::\d{2})?(?:\.\d+)?(?:Z|[+-]\d{2}:?\d{2})?$`),
	regexp.MustCompile(`^\d{10,13}$`),
	regexp.MustCompile(`^` + uuidPattern + `$`),
	regexp.MustCompile(`^` + ulidPattern + `$`),
	regexp.MustCompile(emailPattern),
}

// tokenGrammars match the machine shapes that appear inside a line of data: an id, a
// key, a tag, a host, a file name, an address with its scheme. Two shapes the same
// list would over-reach on are
// not here — a bare slug and a long lowercase token — because the condition that
// makes them machine shapes is a count of characters and the presence of a digit,
// which these two functions hold: isMachineKey and isOpaqueToken.
var tokenGrammars = []*regexp.Regexp{
	regexp.MustCompile(`^` + numeral + `$`),
	regexp.MustCompile(`^` + uuidPattern + `$`),
	regexp.MustCompile(`^` + ulidPattern + `$`),
	regexp.MustCompile(`^` + currency + `$`),
	regexp.MustCompile(`^` + size + `$`),
	regexp.MustCompile(languageTagPattern),
	regexp.MustCompile(emailPattern),
	regexp.MustCompile(hostPattern),
	regexp.MustCompile(filePattern),
	regexp.MustCompile(urlPattern),
}

var (
	machineKey = regexp.MustCompile(`^` + machineKeyPattern + `$`)
	opaque     = regexp.MustCompile(`^` + opaquePattern + `$`)
)

// isMachineKey is a key when it holds a separator *between* the parts of it:
// `task:read`, `AUTH_DENIED`, `text/plain`, `a.b.c`. A separator no sentence uses has
// to be used by one — a colon that ends the token is the colon of a label, "Name:",
// "Status:", "TODO:", and a period that ends it is the period of an abbreviation,
// "e.g.", "a.m."; both are words a person reads and a translator is asked for. So the
// count is of the separators with a word character on *both* sides, and a slug with no
// colon, no underscore and one dot at most is a word, which keeps
// `onboarding-checklist` counted.
func isMachineKey(token string) bool {
	if !machineKey.MatchString(token) {
		return false
	}
	return internal(token, ":_") >= 1 || internal(token, ".") >= 2
}

// internal counts the separators in token that stand between two word characters —
// the ones that join the parts of a key and no other writing.
func internal(token, seps string) int {
	n := 0
	for i := 1; i < len(token)-1; i++ {
		if !strings.ContainsRune(seps, rune(token[i])) {
			continue
		}
		before, after := rune(token[i-1]), rune(token[i+1])
		if isWordRune(before) && isWordRune(after) {
			n++
		}
	}
	return n
}

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

// isOpaqueToken is a session id, a digest, an ETag or a cursor: long, mixed, and
// holding both a letter and a digit, which is what separates a hash from a very long
// English word typed without a space.
func isOpaqueToken(token string) bool {
	if !opaque.MatchString(token) {
		return false
	}
	letters, digits := false, false
	for _, r := range token {
		if unicode.IsDigit(r) {
			digits = true
		} else if unicode.IsLetter(r) {
			letters = true
		}
	}
	return letters && digits
}
