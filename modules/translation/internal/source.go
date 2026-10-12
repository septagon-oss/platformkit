package internal

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/septagon-oss/platformkit/kit/richtext"
	"github.com/septagon-oss/platformkit/modules/translation/contracts"
)

// The two source formats a translatable field can be in, and the one place each
// is split into paragraphs. The digest the two are measured by is
// contracts.Hash, which is the same rule the fake runs; what is here is the
// paragraph split the diff needs and nothing else.
//
// A richtext field goes through kit/richtext, because that is the format whose
// canonical serialisation the write already stores and whose AST decides where
// a block ends. Everything else — a title, a one-line subtitle, a plain
// description — is normalised here and *never* interpreted as markup: a title
// containing an asterisk is a title containing an asterisk, and running it
// through goldmark would rewrite it into a list item. That is why this is not
// simply "richtext with the extensions off".
func paragraphsOf(text string, rich bool) ([]string, error) {
	if rich {
		return richtext.Paragraphs(text)
	}
	return splitBlank(NormalisePlain(text)), nil
}

// Hash is contracts.Hash, named here so this package's callers read as one
// file's rules. One body, two names — see the comment on NormalisePlain below.
func Hash(text string, rich bool) (string, error) { return contracts.Hash(text, rich) }

// NormalisePlain is contracts.NormalisePlain, spelled here so the paragraph
// split below reads as one file's rules. The rule itself lives in contracts/ —
// the fake over there has to hash and normalise by the same rules, and it may not
// import this package — so there is one body and two names for it.
func NormalisePlain(text string) string { return contracts.NormalisePlain(text) }

// splitBlank breaks normalised plain text on blank lines. A plain value can
// contain no fenced block and no table, which is exactly why the plain branch
// may split on a blank line and the richtext branch may not: see
// kit/richtext.Paragraphs.
func splitBlank(normal string) []string {
	if strings.TrimSpace(normal) == "" {
		return nil
	}
	var out []string
	for _, block := range strings.Split(normal, "\n\n") {
		if block = strings.TrimSpace(block); block != "" {
			out = append(out, block)
		}
	}
	return out
}

// paragraphKey is a paragraph's identity: its digest. Two paragraphs with the
// same bytes are the same paragraph, and two that differ by one character are
// not — identity and never similarity, because a diff that *guesses* two
// paragraphs are the same one, edited, can be wrong, and is wrong in the exact
// direction that highlights the wrong text.
func paragraphKey(paragraph string) string {
	sum := sha256.Sum256([]byte(paragraph))
	return hex.EncodeToString(sum[:])
}
