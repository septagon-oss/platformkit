package richtext

import (
	"context"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
)

func TestNormalisePreservesLiteralFencedCodeWhitespace(t *testing.T) {
	source := "```text\nkeep two spaces  \n```"
	before, err := Parse(source)
	if err != nil {
		t.Fatal(err)
	}
	beforeHTML, err := Render(context.Background(), db.Tx[db.Tenant]{}, before, nil, Workspace)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(beforeHTML, "keep two spaces  ") {
		t.Fatalf("source did not exercise literal code whitespace: %q", beforeHTML)
	}
	normal, err := Normalise(source)
	if err != nil {
		t.Fatal(err)
	}
	after, err := Parse(normal)
	if err != nil {
		t.Fatal(err)
	}
	afterHTML, err := Render(context.Background(), db.Tx[db.Tenant]{}, after, nil, Workspace)
	if err != nil {
		t.Fatal(err)
	}
	if beforeHTML != afterHTML {
		t.Fatalf("normalisation changed literal code: before %q, after %q, stored %q", beforeHTML, afterHTML, normal)
	}
}
