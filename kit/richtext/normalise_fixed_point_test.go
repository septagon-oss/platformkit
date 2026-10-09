package richtext

import (
	"context"
	"errors"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
)

func TestNormaliseDoesNotStoreTextItsValidatorRefuses(t *testing.T) {
	source := "<p\t"
	stored, err := Normalise(source)
	if err != nil {
		var refused *Refused
		if !errors.As(err, &refused) || len(refused.Issues) == 0 ||
			refused.Issues[0].Construct != "raw HTML" || refused.Issues[0].Line != 1 {
			t.Fatalf("first-write refusal = %v, want raw HTML on line 1", err)
		}
		return
	}
	sourceDoc, err := Parse(source)
	if err != nil {
		t.Fatal(err)
	}
	storedDoc, err := Parse(stored)
	if err != nil {
		t.Fatal(err)
	}
	before, err := Render(context.Background(), db.Tx[db.Tenant]{}, sourceDoc, nil, Workspace)
	if err != nil {
		t.Fatal(err)
	}
	after, err := Render(context.Background(), db.Tx[db.Tenant]{}, storedDoc, nil, Workspace)
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Errorf("storing accepted text changed the rendered document: %q became %q", before, after)
	}
	again, err := Normalise(stored)
	if err != nil {
		t.Fatalf("accepted source %q was stored as %q, which the next write refuses: %v", source, stored, err)
	}
	if again != stored {
		t.Fatalf("accepted source %q was stored as %q, then changed to %q", source, stored, again)
	}
}
