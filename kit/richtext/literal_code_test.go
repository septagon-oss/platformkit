package richtext

import "testing"

func TestSupportedCodeMayContainTextThatLooksLikeAnotherConstruct(t *testing.T) {
	for _, source := range []string{
		"Use `:smile:` as literal text.",
		"```text\n[^note] :smile: $x$\n```",
	} {
		t.Run(source, func(t *testing.T) {
			if _, err := Normalise(source); err != nil {
				t.Fatalf("code content was refused: %v", err)
			}
		})
	}
}
