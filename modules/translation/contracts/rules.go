package contracts

// The rules a translation is judged by, as pure functions of their arguments.
//
// They live in contracts/ and not in internal/ for one reason: the port has two
// implementations — the service over Postgres and the fake every consumer tests
// against — and both have to run *these* rules, not each their own. The same
// rules inside internal/ would mean the fake imports internal/, which is the
// import another module is not allowed to make and which would link the real
// implementation into every consumer's test binary. So the rule is declared once,
// here, next to the interface it decides the status of, and both sides call it.
//
// Nothing here touches a database, a clock or a request. `reviewed` and
// `sourceStillMatches` arrive as arguments because who knows them differs: the
// read knows the source it is holding, the overview knows the row it loaded, and
// a rule that reached for either would be a rule one of the two could not run.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/richtext"
)

// ErrNoMachine is the refusal a suggestion gets in an installation with no
// provider. It is in contracts because both implementations answer a suggestion
// with it, and a message two packages spell separately is two messages waiting
// to disagree — which is the whole failure mode a conformance suite exists to
// prevent.
var ErrNoMachine = fmt.Errorf("%w: this installation serves no machine translation", crud.ErrInvalid)

// Hash is the digest staleness is decided by. The two source formats are hashed
// by their own normalisation, so a stored hash only ever compares against a
// source put through the same rules — the one property the whole stale mechanism
// rests on.
//
// A richtext field goes through kit/richtext, because that is the format whose
// canonical serialisation the write already stores and whose AST decides where a
// block ends. Everything else — a title, a one-line subtitle, a plain
// description — is normalised here and *never* interpreted as markup: a title
// containing an asterisk is a title containing an asterisk, and running it
// through goldmark would rewrite it into a list item. That is why this is not
// simply "richtext with the extensions off".
func Hash(text string, rich bool) (string, error) {
	if rich {
		return richtext.SourceHash(text)
	}
	sum := sha256.Sum256([]byte(NormalisePlain(text)))
	return hex.EncodeToString(sum[:]), nil
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
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " \t")
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

// StateOf is the overview's whole rule, in one expression.
//
// A machine draft is counted in a column of its own and never as complete: an
// "Up-to-date" badge on text nobody has read is the exact lie this screen
// exists to avoid. And a source the caller could not measure (sourceStillMatches
// false because the hash failed or the source is gone) is a source no translation
// may be trusted against, which is outdated and not complete.
func StateOf(hasRow, unreviewedMachine, sourceStillMatches bool) string {
	switch {
	case !hasRow:
		return rest.StateMissing
	case !sourceStillMatches:
		return rest.StateOutdated
	case unreviewedMachine:
		return rest.StateMachine
	default:
		return rest.StateComplete
	}
}

// StatusOf is the write's own status column, from the two facts the write knows:
// who produced this text and whether a person has read it since.
//
// The `reviewed` argument is what keeps a person's edit of a machine draft from
// being published as a draft nobody looked at: the provenance stays machine —
// that is what provenance means — and the text is now somebody's answer, which is
// the difference the public door turns on.
func StatusOf(origin string, reviewed, stale bool) string {
	switch {
	case stale:
		return rest.FallbackOutdated
	case origin == rest.OriginMachine && !reviewed:
		return rest.FallbackMachine
	default:
		return rest.StateComplete
	}
}
