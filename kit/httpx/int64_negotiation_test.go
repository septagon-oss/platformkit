package httpx

import "testing"

func TestDecimalNegotiationRequiresAnExplicitPreference(t *testing.T) {
	for _, accept := range []string{"", "*/*", "application/*", "application/json", int64JSONMediaType + ";q=0", int64JSONMediaType + ", application/json", int64JSONMediaType + ";q=0.5, application/json;q=0.9"} {
		if decimalAccepted(accept) {
			t.Errorf("selected decimal for %q", accept)
		}
	}
	for _, accept := range []string{int64JSONMediaType, int64JSONMediaType + ", */*", int64JSONMediaType + ";q=0.9, application/json;q=0.5"} {
		if !decimalAccepted(accept) {
			t.Errorf("did not select decimal for %q", accept)
		}
	}
}
