package richtext

import (
	"context"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
)

func TestAcceptedExternalLinkHasDestinationAndSafetyAttributes(t *testing.T) {
	normal, err := Normalise("[example](HTTPS://example.test/page)")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := Parse(normal)
	if err != nil {
		t.Fatal(err)
	}
	html, err := Render(context.Background(), db.Tx[db.Tenant]{}, doc, nil, Workspace)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToLower(html), `href="https://example.test/page"`) ||
		!strings.Contains(html, `rel="noopener noreferrer nofollow ugc"`) {
		t.Fatalf("accepted external link lost its destination or safety attributes: %q", html)
	}
}
