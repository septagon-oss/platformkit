package internal

// RFC 6238's own vectors, checked against this file's own code.
//
// The Appendix B table of the RFC, for the SHA-1 secret "12345678901234567890"
// at the times it names. The published codes are eight digits; this module uses
// six (contracts.TOTPDigits), and the RFC's truncation is a modulus, so the six
// are the last six of the eight. A vector is worth more here than a round trip
// through the module's own generator would be: an implementation that is wrong
// in the same way as its own test would pass a test written that way.

import (
	"encoding/base32"
	"strings"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

// vectorSecret is RFC 6238 Appendix B's shared secret, as the base32 an
// enrolment would have handed out. Composed here rather than pasted, so a wrong
// paste cannot quietly move the vectors.
var vectorSecret = base32.StdEncoding.WithPadding(base32.NoPadding).
	EncodeToString([]byte("12345678901234567890"))

func TestRFC6238Vectors(t *testing.T) {
	secret, err := decodeTOTPSecret(strings.ToLower(vectorSecret))
	if err != nil {
		t.Fatalf("the RFC's own test secret does not decode: %v", err)
	}
	for _, table := range []struct {
		seconds int64
		want    string
	}{
		{59, "94287082"},
		{1111111109, "07081804"},
		{1111111111, "14050471"},
		{1234567890, "89005924"},
		{2000000000, "69279037"},
		{20000000000, "65353130"},
	} {
		at := time.Unix(table.seconds, 0).UTC()
		got, err := totpCode(secret, totpStep(at))
		if err != nil {
			t.Fatalf("code at %d: %v", table.seconds, err)
		}
		if !strings.HasSuffix(table.want, got) {
			t.Errorf("TOTP at t=%d = %s, want the last %d digits of %s",
				table.seconds, got, contracts.TOTPDigits, table.want)
		}
	}
}

func TestATOTPMatchesItsWindowAndRefusesItsPast(t *testing.T) {
	secret, err := decodeTOTPSecret(vectorSecret)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Unix(1_200_000_000, 0).UTC()
	now := totpStep(at)
	code := func(step int64) string {
		got, err := totpCode(secret, step)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	for _, table := range []struct {
		name      string
		seconds   int64
		lastStep  int64
		wantMatch bool
		wantStep  int64
	}{
		{"this step", now * 30, 0, true, now},
		{"one step early", (now - contracts.TOTPSkew) * 30, 0, true, now - contracts.TOTPSkew},
		{"one step late", (now + contracts.TOTPSkew) * 30, 0, true, now + contracts.TOTPSkew},
		{"two steps late is outside the window", (now + contracts.TOTPSkew + 1) * 30, 0, false, 0},
		{"a code from a step already spent", (now - contracts.TOTPSkew) * 30, now - contracts.TOTPSkew, false, 0},
	} {
		// The code is from the time the table names; the moment the answer
		// arrives at is always the outer `at`. Moving both together would ask the
		// window a question it always answers the same way.
		codeAt := time.Unix(table.seconds, 0).UTC()
		step, ok := totpMatches(secret, code(totpStep(codeAt)), at, table.lastStep)
		if ok != table.wantMatch || step != table.wantStep {
			t.Errorf("%s: match returned (%v, %d), want (%v, %d)",
				table.name, ok, step, table.wantMatch, table.wantStep)
		}
	}
	if _, ok := totpMatches(secret, "12345", at, 0); ok {
		t.Error("a five-digit answer matched: the code length is not a suggestion")
	}
}

func TestSealAndOpenRoundTripAndRefuseAnotherKey(t *testing.T) {
	const secret = "JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP"
	sealed, err := seal([]byte("the deployment's key"), []byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	if string(sealed) == secret || strings.Contains(string(sealed), secret) {
		t.Fatal("the sealed secret contains the secret: sealing did nothing")
	}
	opened, err := openSecret([]byte("the deployment's key"), sealed)
	if err != nil || string(opened) != secret {
		t.Fatalf("open = %q, %v", opened, err)
	}
	if _, err := openSecret([]byte("a different key"), sealed); err == nil {
		t.Error("a secret sealed with another key opened under this one")
	}
	if _, err := seal(nil, []byte(secret)); err != contracts.ErrNoFactorKey {
		t.Errorf("sealing with no key = %v, want %v", err, contracts.ErrNoFactorKey)
	}
}
