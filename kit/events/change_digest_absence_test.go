package events_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/events"
)

// sealed is a row with one secret-shaped field the trail may only carry as a digest,
// once as a value and once as a pointer.
type sealed struct {
	Secret string  `json:"secret" audit:"digest"`
	Token  *string `json:"token,omitempty" audit:"digest"`
}

// TestADigestOfNothingIsNotAChange holds the absence rule for a digested field as
// Changes already holds it for a plain one: a create whose secret is empty carries no
// change for it, and a field that went from absent (null) to empty did not move. The
// digest of an empty value is still a value, and a reader would read "it held
// something" where it held nothing.
func TestADigestOfNothingIsNotAChange(t *testing.T) {
	got, err := events.Changes(nil, &sealed{}, "secret", "token")
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("a create with no secret carried %+v, want no change", got)
	}
	empty := ""
	got, err = events.Changes(&sealed{}, &sealed{Token: &empty}, "token")
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("a token that went from absent to empty carried %+v, want no change", got)
	}
	got, err = events.Changes(&sealed{}, &sealed{Secret: "s3cret"}, "secret")
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if len(got) != 1 || len(got[0].Before) != 0 || len(got[0].After) == 0 {
		t.Errorf("a secret set for the first time carried %+v, want an after half and no before half", got)
	}
}
