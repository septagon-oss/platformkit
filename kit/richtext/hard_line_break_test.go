package richtext

import (
	"context"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
)

func TestNormalisePreservesSpaceMarkedHardBreak(t *testing.T) {
	source := "First line  \nSecond line"
	doc, err := Parse(source)
	if err != nil {
		t.Fatal(err)
	}
	before, err := Render(context.Background(), db.Tx[db.Tenant]{}, doc, nil, Workspace)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(before, "<br") {
		t.Fatalf("source did not exercise a hard line break: %q", before)
	}

	normal, err := Normalise(source)
	if err != nil {
		t.Fatal(err)
	}
	doc, err = Parse(normal)
	if err != nil {
		t.Fatal(err)
	}
	after, err := Render(context.Background(), db.Tx[db.Tenant]{}, doc, nil, Workspace)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(after, "<br") {
		t.Fatalf("normalisation removed the hard line break: source %q, stored %q, rendered %q", source, normal, after)
	}
}
