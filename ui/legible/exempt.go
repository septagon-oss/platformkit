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
// A hyphen never exempts on its own: `Hard-coded`, `Sign-in` and `Follow-up` all
// count as copy. The residual risk is stated where it can be read — `of-PT` passes as
// a language tag and `onboarding-2026-checklist` as an opaque token — and it is
// bounded by the report printing every exempt string, so nothing the number chose not
// to count is hidden.
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
	// An address of any kind — a path, a media type, a URL — is data. Nothing
	// written as copy needs a slash to say it.
	if strings.Contains(token, "/") {
		return true
	}
	for _, grammar := range tokenGrammars {
		if grammar.MatchString(token) {
			return true
		}
	}
	return false
}

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
)

// wholeGrammars match an amount, an instant or a rate that spans spaces — a money
// cell, a timestamp, a percentage. They are tried against the whole string because
// splitting "R$ 1.234,50" on its space would ask whether "R$" is a word.
//
// Dates are exempt in the numeric forms this kernel renders, and only those: a page
// that spells "Monday, 4 October 2026" is a page whose month names nobody translated,
// which is a decision about that page and not something this rule hides.
var wholeGrammars = []*regexp.Regexp{
	regexp.MustCompile(`^` + numeral + `$`),
	regexp.MustCompile(`^[-+−]?\d+(?:[.,]\d+)? ?%$`),
	regexp.MustCompile(`^[-+−]?(?:` + currency + `) ?[-+−]?[\d .,]+$`),
	regexp.MustCompile(`^[-+−]?[\d .,]+ ?(?:` + currency + `)$`),
	regexp.MustCompile(`^[\d .,]+ ?` + size + `$`),
	regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`),
	regexp.MustCompile(`^\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}(?::\d{2})?(?:\.\d+)?(?:Z|[+-]\d{2}:?\d{2})?$`),
	regexp.MustCompile(`^\d{10,13}$`),
	regexp.MustCompile(`^[A-Z][a-z]{2} \d{1,2} [A-Z][a-z]{2} (?:\d{4} )?\d{2}:\d{2}(?::\d{2})? (?:UTC|GMT|Z|[+-]\d{4})$`),
	regexp.MustCompile(`^` + uuidPattern + `$`),
	regexp.MustCompile(`^` + ulidPattern + `$`),
	regexp.MustCompile(emailPattern),
}

// tokenGrammars match the machine shapes that appear inside a line of data: an id, a
// key, a tag, a host, a file name. Two shapes the same list would over-reach on are
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
}

var (
	machineKey = regexp.MustCompile(`^` + machineKeyPattern + `$`)
	opaque     = regexp.MustCompile(`^` + opaquePattern + `$`)
)

// isMachineKey is a key when it holds a separator no sentence uses: `task:read`,
// `AUTH_DENIED`, `text/plain`, `a.b.c`. A slug with no colon, no underscore and one
// dot at most is a word, and `onboarding-checklist` stays counted.
func isMachineKey(token string) bool {
	if !machineKey.MatchString(token) {
		return false
	}
	return strings.ContainsAny(token, ":_") || strings.Count(token, ".") >= 2
}

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
