package httpx_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/httpx"
)

func TestCookieNamesKeepTheirApplicationAndHostPrefixes(t *testing.T) {
	for _, tc := range []struct {
		app                 appname.Name
		base, plain, secure string
	}{
		{"", "session", "session", "__Host-session"},
		{"", "preferences", "preferences", "__Host-preferences"},
		{"", "", "", "__Host-"},
		{"pkit", "session", "pkit-session", "__Host-pkit-session"},
		{"pkit", "preferences", "pkit-preferences", "__Host-pkit-preferences"},
		{"pkit", "", "pkit-", "__Host-pkit-"},
		{"billing-service", "session", "billing-service-session", "__Host-billing-service-session"},
		{"billing-service", "preferences", "billing-service-preferences", "__Host-billing-service-preferences"},
		{"billing-service", "", "billing-service-", "__Host-billing-service-"},
		{"BAD", "session", "-session", "__Host--session"},
		{"BAD", "preferences", "-preferences", "__Host--preferences"},
		{"BAD", "", "-", "__Host--"},
	} {
		for secure, want := range map[bool]string{false: tc.plain, true: tc.secure} {
			if got := httpx.CookieNameFor(tc.app, tc.base, secure); got != want {
				t.Errorf("CookieNameFor(%q, %q, %v) = %q, want %q", tc.app, tc.base, secure, got, want)
			}
			if got := httpx.CookieNameOf(tc.app, tc.base, secure); got != want {
				t.Errorf("CookieNameOf = %q, want %q", got, want)
			}
			if tc.app == "" {
				if got := httpx.CookieName(tc.base, secure); got != want {
					t.Errorf("CookieName = %q, want %q", got, want)
				}
			}
		}
	}
}
