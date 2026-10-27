package resource

// line_initial_noun_test.go pins the one transformation a resource noun undergoes
// before the page prints it: the letter that begins a line. Everything else about
// the word — its second word, its accents, its script, its case — is the author's
// and leaves unchanged.

import "testing"

func TestTheLetterThatStartsALineIsThePagesAndEveryOtherLetterIsTheAuthors(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ word, want string }{
		// The shapes a policy actually holds: the noun as written, the noun someone
		// already capitalised, a phrase whose second word stays lower, and the
		// accented and non-Latin first letters a translation serves.
		{"notes", "Notes"},
		{"Notes", "Notes"},
		{"resolved rows", "Resolved rows"},
		{"tarefas", "Tarefas"},
		{"ämnen", "Ämnen"},
		{"απογραφή", "Απογραφή"},
		{"注文", "注文"},
		// The two shapes no resolved policy can hold. kit/entity/present fills the
		// plural from the singular when the author declared none, so a renderer
		// reaches here with a word; neither answer below is a page, and both say
		// the rule adds a letter rather than rewriting or discarding a byte.
		{"", ""},
		{"\xffunowned", "\xffunowned"},
	} {
		if got := startSentence(c.word); got != c.want {
			t.Errorf("startSentence(%q) = %q, want the same word with its line begun as %q", c.word, got, c.want)
		}
	}
}
