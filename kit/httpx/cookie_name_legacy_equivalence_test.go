package httpx_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/httpx"
)

func TestTheZeroApplicationCookieNameIsTheLegacyCookieName(t *testing.T) {
	for _, secure := range []bool{true, false} {
		if got, want := httpx.CookieNameFor("", "lang", secure), httpx.CookieName("lang", secure); got != want {
			t.Errorf("CookieNameFor(\"\", lang, %v) = %q, want %q", secure, got, want)
		}
	}
}
