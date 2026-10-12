package internal

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
)

// secretBytes is the width of a Standard Webhooks secret: 32 random bytes, which
// is the key width HMAC-SHA256 wants and the width AES-256-GCM requires, so the
// same number serves both.
const secretBytes = 32

// secretPrefix is Standard Webhooks' own spelling. The module emits it because a
// customer's existing receiver library verifies a whsec_ secret unchanged, which
// is the entire point of following the specification.
const secretPrefix = "whsec_"

// signingKey is the 32 bytes a whsec_ secret decodes to: what HMAC keys with.
// Standard Webhooks base64-encodes them inside the prefix and a receiver does the
// same, so a signature computed over these bytes is the signature their library
// checks. These bytes — and not their shown spelling — are what this module
// seals, so one encoding mistake cannot survive into the stored secret.
type signingKey []byte

// version is the one signature version Standard Webhooks defines, and the prefix
// a receiver's library looks its entry up by.
const version = "v1"

// newKey returns 32 fresh bytes and their whsec_ spelling.
func newKey(r io.Reader) (signingKey, string, error) {
	raw := signingKey(make([]byte, secretBytes))
	if _, err := r.Read(raw); err != nil {
		return nil, "", fmt.Errorf("webhook: mint a signing secret: %w", err)
	}
	return raw, secretPrefix + base64.StdEncoding.EncodeToString(raw), nil
}

// sign is the Standard Webhooks signature: HMAC-SHA256 over
// webhook-id + "." + webhook-timestamp + "." + body, base64-encoded, prefixed
// with the version the receiver's check reads.
//
// The spelling is "v1,<base64>", and the comma is the whole finding: the
// specification's header is a space-delimited list of "<version>,<signature>"
// entries, and the reference library splits the header on spaces and then
// requires a comma in each part. A header written "v1 <base64>" is one the
// library reads as a version with no signature and rejects with "no matching
// signature found", so the space would be a module that signs correctly and is
// refused by every standard receiver. The list form is also what leaves room for
// a second entry — a rotated-out key, a v2 — without changing the header's name.
//
// The body is the stored payload's bytes, so every attempt at one event signs
// the same string — which is the property the retry ladder depends on and the
// one a test can falsify.
func sign(key signingKey, id uuid.UUID, at time.Time, body []byte) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(id.String()))
	mac.Write([]byte("."))
	mac.Write([]byte(fmt.Sprintf("%d", at.Unix())))
	mac.Write([]byte("."))
	mac.Write(body)
	return version + "," + base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// box wraps a signing key for rest: AES-256-GCM under the deployment's key. The
// plaintext never touches the database, and this is the one place the wrap is
// written, so a future kit/secrets has exactly this to replace.
type box struct {
	gcm cipher.AEAD
}

// newBox prepares the deployment key. A key of the wrong width is refused here,
// at composition, rather than at the first secret written.
func newBox(key []byte) (*box, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("webhook: the secret key is not an AES-256 key: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("webhook: %w", err)
	}
	return &box{gcm: gcm}, nil
}

// seal returns the nonce and the ciphertext, two columns, because a nonce has no
// meaning apart from the ciphertext beside it and concatenating them would put
// the pair's shape in a comment instead of a schema.
func (b *box) seal(plain []byte) (nonce, ciphertext []byte, err error) {
	nonce = make([]byte, b.gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, fmt.Errorf("webhook: seal a signing secret: %w", err)
	}
	return nonce, b.gcm.Seal(nil, nonce, plain, nil), nil
}

// open is the other half, and the additional authenticated data is empty on both
// sides: the endpoint id is not in the seal, so a ciphertext cannot be moved
// between rows by an attacker, but nothing else about it is bound either.
func (b *box) open(nonce, ciphertext []byte) ([]byte, error) {
	plain, err := b.gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("webhook: open a signing secret: %w", err)
	}
	return plain, nil
}
