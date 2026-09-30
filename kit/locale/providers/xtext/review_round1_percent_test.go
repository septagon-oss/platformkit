package xtext_test

// A reviewer's case, T-0111 review round 1.
//
// `verbs` reads a copy's format specifiers with the regular expression
// `%[-+# 0]*[0-9.]*[a-zA-Z]`, and the copy it compares is the gettext source text,
// in which a literal percent sign is written `%%`. The expression has no idea that
// an escaped pair is one character: it restarts inside the pair, and the space and
// the first letter that follow become a verb. So:
//
//	"100%% sure, %d items"   →  verbs "% s %d"
//	"100%% certo, %d itens"  →  verbs "% c %d"
//
// two copies that interpolate the same one argument in the same order, refused at
// composition for changing them ("xtext: case/pt-PT.json says \"% c %d\" about
// \"k\" where en.json says \"% s %d\""). The refusal is not merely loud: it points
// a translator at the wrong line, and `%%` is the only way to spell what they meant
// — every copy that takes the advice this refusal gives is a copy this gate then
// refuses.
//
// The two rows without an escaped percent are the correct half of the same check
// and stay out of this file on purpose: "Save 20% on %d items" really does ask
// fmt for two arguments, because `% o` is octal-with-a-space-flag, and refusing it
// is the guard being right.
//
// The assertion below is the fixed behaviour: the escaped catalogue loads, and both
// languages print the one number they are given and the one percent sign they asked
// for.

import (
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/locale"
	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
)

func TestACopyThatEscapesItsPercentSignLoadsAndPrintsIt(t *testing.T) {
	var messages xtext.Catalog
	func() {
		defer func() {
			if refused := recover(); refused != nil {
				t.Errorf("a catalogue that escapes its percent signs was refused: %v", refused)
			}
		}()
		messages = xtext.Load("en", xtext.Source{
			Name: "the case",
			FS: fstest.MapFS{
				"en.json":    &fstest.MapFile{Data: []byte(`{"checkout.discount": {"translation": "100%% sure, %d items"}}`)},
				"pt-PT.json": &fstest.MapFile{Data: []byte(`{"checkout.discount": {"translation": "100%% certo, %d itens"}}`)},
			},
		})
	}()
	if messages == nil {
		t.FailNow()
	}

	for _, c := range []struct{ preference, want string }{
		{"en", "100% sure, 3 items"},
		{"pt-PT", "100% certo, 3 itens"},
	} {
		got := locale.SelectLocale(messages, c.preference).Text("checkout.discount", "100%% sure, %d items", 3)
		if got != c.want {
			t.Errorf("%s printed %q, want %q", c.preference, got, c.want)
		}
	}
}
