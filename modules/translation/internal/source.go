package internal

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/septagon-oss/platformkit/kit/richtext"
)

// The two source formats a translatable field can be in, and the one place each
// is normalised, hashed and split into paragraphs.
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

// Hash is the digest staleness is decided by. The two formats are hashed by
// their own normalisation, so a stored hash only ever compares against a source
// put through the same rules — which is the one property the whole stale
// mechanism rests on.
func Hash(text string, rich bool) (string, error) {
	if rich {
		return richtext.SourceHash(text)
	}
	return plainHash(NormalisePlain(text)), nil
}

// NormalisePlain is the plain-text normalisation: LF newlines, no trailing
// spaces, no run of more than one blank line, no leading or trailing blank
// lines, and no interpretation of a single character as markup.
//
// It is the shape a person's textarea holds after they stop typing, and it is
// what makes a translation written from "a\r\nb" and a source saved as "a\nb"
// the same source.
func NormalisePlain(text string) string {
	s := strings.ReplaceAll(text, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	out := make([]string, 0, len(lines))
	blank := 0
	for _, line := range lines {
		if line == "" {
			blank++
			if blank > 1 {
				continue
			}
		} else {
			blank = 0
		}
		out = append(out, line)
	}
	for len(out) > 0 && out[0] == "" {
		out = out[1:]
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return strings.Join(out, "\n")
}

// plainHash is SHA-256 of normalised plain text in lowercase hex — the same
// shape, 64 characters, as the richtext digest, because the column stores one
// kind of thing and a CHECK on its length is the half that cannot be a comment.
func plainHash(normal string) string {
	sum := sha256.Sum256([]byte(normal))
	return hex.EncodeToString(sum[:])
}

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
