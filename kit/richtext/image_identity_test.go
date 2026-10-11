package richtext

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
)

func TestImageProviderCannotSubstituteAnotherFile(t *testing.T) {
	id := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	other := uuid.MustParse("550e8400-e29b-41d4-a716-446655440001")
	source := "![alt](pk-file:" + id.String() + ")"
	path := "/api/v1/file/files/" + other.String() + "/content"
	files := testFiles{image: Image{Src: path, SrcSet: path + " 800w", Width: 800, Height: 600}}

	if stored, err := Prepare(context.Background(), db.Tx[db.Tenant]{}, source, files, 100); err == nil || stored != "" {
		t.Fatalf("substituted image stored as %q: %v", stored, err)
	}
	doc, err := Parse(source)
	if err != nil {
		t.Fatal(err)
	}
	if html, err := Render(context.Background(), db.Tx[db.Tenant]{}, doc, files, Workspace); err == nil || html != "" {
		t.Fatalf("substituted image rendered as %q: %v", html, err)
	}
}
