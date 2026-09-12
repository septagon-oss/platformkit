package main

import (
	"context"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/ui/page"
)

// An explicit sign-in URL wins over browser negotiation. Ambiguous query
// values do not select a language, and no preference is persisted implicitly.
func loginLocale(ctx context.Context, _ page.Request) string {
	if request, ok := httpx.RequestFrom(ctx); ok {
		if values := request.URL.Query()["lang"]; len(values) == 1 {
			return values[0]
		}
	}
	return ""
}
