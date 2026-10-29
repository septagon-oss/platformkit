package secretstest

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/septagon-oss/platformkit/kit/secrets"
)

// Keys runs the envelope cases: the identity, the nonce, the binding to the address,
// and the refusals. The stores that hold their own key material run all of it; the
// one that reaches a key service runs the same list against that service, which is
// the point of writing the cases once.
func Keys(t *testing.T, a Adapter) {
	t.Helper()
	plaintext := []byte("a credential worth sealing")
	key := secrets.Shared(SuiteKey)
	provision := func(t *testing.T) {
		t.Helper()
		a.WriteKey(t, key)
		a.WriteKey(t, secrets.Shared(SuiteKeyOther))
	}

	t.Run("a seal opens, and two seals differ", func(t *testing.T) { sealRoundTrip(t, a, plaintext, key, provision) })
	t.Run("a seal is bound to the address it was written for", func(t *testing.T) { sealAddress(t, a, plaintext, key, provision) })
	t.Run("bytes this port did not write are refused", func(t *testing.T) { refuseForeignBytes(t, a, plaintext, key, provision) })
	t.Run("empty plaintext is refused", func(t *testing.T) { refuseEmptyPlaintext(t, a, key, provision) })
	t.Run("a seal does not carry the plaintext", func(t *testing.T) { hidePlaintext(t, a, plaintext, key, provision) })
}

// C8. Seal then Open is the identity, and two seals of one plaintext are
// different bytes that both open. A fixed nonce is a two-time pad over one key —
// the single worst bug an AEAD adapter can have — and a nonce that is random but
// not stored opens as garbage.
func sealRoundTrip(t *testing.T, a Adapter, plaintext []byte, key secrets.Ref, provision func(*testing.T)) {
	provision(t)
	c := a.Store(t)
	ref := key
	first, err := c.Seal(context.Background(), ref, plaintext)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	second, err := c.Seal(context.Background(), ref, plaintext)
	if err != nil {
		t.Fatalf("a second Seal: %v", err)
	}
	if bytes.Equal(first, second) {
		t.Error("two seals of one plaintext are the same bytes, which means one nonce")
	}
	for i, sealed := range [][]byte{first, second} {
		got, err := c.Open(context.Background(), ref, sealed)
		if err != nil {
			t.Fatalf("Open of seal %d: %v", i, err)
		}
		if !bytes.Equal(got, plaintext) {
			t.Errorf("Open of seal %d = % q; want % q", i, got, plaintext)
		}
	}
}

// C9. The address is in the seal: a ciphertext written for one name does not
// open under another. This is the gap webhook's own box records
// for itself — nothing about the row a ciphertext sits beside is bound into it —
// and the reason this port writes the AAD rather than nothing.
func sealAddress(t *testing.T, a Adapter, plaintext []byte, key secrets.Ref, provision func(*testing.T)) {
	provision(t)
	c := a.Store(t)
	sealed, err := c.Seal(context.Background(), key, plaintext)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if _, err := c.Open(context.Background(), secrets.Shared(SuiteKeyOther), sealed); !errors.Is(err, secrets.ErrSealed) {
		t.Errorf("Open under another key's name = %v; want ErrSealed", err)
	}
}

// C10. What Open refuses: a truncated seal, random bytes, and a seal made under
// a different key. Each is a column full of bytes that are not this port's, and
// each must be refused rather than opened into garbage.
func refuseForeignBytes(t *testing.T, a Adapter, plaintext []byte, key secrets.Ref, provision func(*testing.T)) {
	provision(t)
	c := a.Store(t)
	ref := key
	sealed, err := c.Seal(context.Background(), ref, plaintext)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	for name, bytes := range map[string][]byte{
		"truncated": sealed[:len(sealed)/2],
		"random":    []byte("not a seal at all, but a column somebody wrote text into"),
		"empty":     nil,
		"plaintext": plaintext,
		"other key": otherKeySeal(t, plaintext),
		"too short": []byte{0x01, 0x02},
	} {
		if _, err := c.Open(context.Background(), ref, bytes); !errors.Is(err, secrets.ErrSealed) {
			t.Errorf("Open of %s = %v; want ErrSealed", name, err)
		}
	}
}

// C11. Sealing nothing is refused rather than sealed: an envelope around empty
// plaintext is an oracle for "is this secret empty" and a row of ciphertext that
// stands for nothing.
func refuseEmptyPlaintext(t *testing.T, a Adapter, key secrets.Ref, provision func(*testing.T)) {
	provision(t)
	c := a.Store(t)
	for _, empty := range [][]byte{nil, {}} {
		if _, err := c.Seal(context.Background(), key, empty); !errors.Is(err, secrets.ErrSealed) {
			t.Errorf("Seal of %d bytes = %v; want ErrSealed", len(empty), err)
		}
	}
}

// C12. A seal is not the plaintext: neither the bytes nor their base64 appear in
// it. An adapter that base64s and calls it sealing is a bug no reader can detect
// and one case catches.
func hidePlaintext(t *testing.T, a Adapter, plaintext []byte, key secrets.Ref, provision func(*testing.T)) {
	provision(t)
	c := a.Store(t)
	sealed, err := c.Seal(context.Background(), key, plaintext)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if bytes.Contains(sealed, plaintext) {
		t.Error("the sealed bytes carry the plaintext")
	}
	if len(sealed) <= 1+12 {
		t.Errorf("a seal of %d bytes is shorter than a version byte, a nonce and a tag", len(sealed))
	}
}
