package richtext

import "testing"

func TestEscapedUnsupportedSyntaxRemainsLiteralText(t *testing.T) {
	for _, source := range []string{`\:smile:`, `\[^note]`} {
		t.Run(source, func(t *testing.T) {
			if _, err := Normalise(source); err != nil {
				t.Fatalf("escaped text was refused as syntax: %v", err)
			}
		})
	}
}
