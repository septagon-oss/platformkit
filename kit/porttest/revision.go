package porttest

import (
	"reflect"
	"strings"
)

// RevisionField is this package's reading of "this entity carries a revision": the
// first field of typ, or of anything it embeds, named as a revision a caller quotes
// back — see isRevision. A name that merely contains one of those words
// (Conversion) is not one, because a reading that flags everything a suite might
// have meant teaches a suite to ignore the result.
//
// It exists because a suite that declines the Stale case does so in prose, and a
// reason is the one claim about a port no check in this package reads — the
// harness requires a Skip to have a sentence, not a true one. A suite that says "no
// row of mine carries a revision" can watch that sentence itself: call this on the
// stored entity in a case of its own and let it redden when the kernel grows the
// field, which is the moment the decline has to become the case. What the harness
// cannot see is the entity type — W is the module's fixture, and the suite's
// description never names one — so the watching is the suite's and the reading is
// shared here, beside the Kind it is about.
func RevisionField(typ reflect.Type) (reflect.StructField, bool) {
	if typ.Kind() != reflect.Struct {
		return reflect.StructField{}, false
	}
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if isRevision(f.Name) {
			return f, true
		}
		if f.Anonymous {
			if got, ok := RevisionField(f.Type); ok {
				return got, true
			}
		}
	}
	return reflect.StructField{}, false
}

// isRevision reports whether a field name is a revision, or ends with one at a word
// boundary: Revision, RowVersion, ETag. "Conversion" ends with the letters of a
// version and is not one, which is why the boundary is asked and not the suffix
// alone — and a reading that called it one teaches a suite to ignore the answer.
func isRevision(name string) bool {
	for _, word := range []string{"revision", "version", "etag"} {
		rest, cut := strings.CutSuffix(strings.ToLower(name), word)
		if !cut {
			continue
		}
		if rest == "" {
			return true
		}
		if b := name[len(rest)]; b >= 'A' && b <= 'Z' { // the word starts a word of its own
			return true
		}
	}
	return false
}
