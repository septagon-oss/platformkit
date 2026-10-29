// Package box is the envelope the adapters that hold their own key write:
// AES-256-GCM over key material the store hands over, with the secret's address
// in the additional authenticated data.
//
// env and file seal with the same algorithm over the same material, and two
// handings of one AEAD are two ways to get one wrong, so the body is shared and
// internal: no other tree may import it. A store that runs a key service of its
// own must not be handed this either — a crypto path in front of a key store is
// a second truth about which key a ciphertext sits under, and that store's own
// answer is the one that rotates.
//
// The AAD is what this package adds to a plain AEAD: a ciphertext written for
// one name, or one tenant, does not open under another.
package box

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

// ErrMaterial is key material this package cannot use, and ErrEnvelope is bytes
// this package did not write. They are this package's own because the port's
// sentinel lives above it — an envelope that named the port would be an internal
// package depending on the package that exists to wrap it — and kit/secrets turns
// both into ErrSealed, with the address in the sentence.
var (
	ErrMaterial = errors.New("the key material is not base64 of an AES-256 key")
	ErrEnvelope = errors.New("these bytes are not a v1 envelope")
)

// keyBytes is AES-256's own width: the key this envelope takes, and the width the
// deployment key that wraps a webhook signing secret already has.
const keyBytes = 32

// version is the one byte every seal this package writes begins with. It is there
// for the day a second envelope exists, so that a reader can tell the two apart
// rather than guessing from a length: an old ciphertext must still be readable when
// the new one is written.
const version = 0x01

// Box is one prepared key. It is safe to share: cipher.AEAD is.
type Box struct {
	gcm cipher.AEAD
}

// Key turns the bytes a store holds under a key's name into a Box.
//
// The material is base64.StdEncoding of exactly 32 bytes, with surrounding
// whitespace trimmed because a mounted secret file ends in a newline its producer
// wrote. Anything else — a 16-byte key, a raw key that is not base64, an empty
// file — is refused here, at the first call that needed the key, with ErrSealed:
// a wrong-width key is a defect in the deployment and it should be named the first
// time anything asks for it, not panicked on the way.
func Key(material []byte) (*Box, error) {
	raw, err := base64.StdEncoding.DecodeString(string(bytes.TrimSpace(material)))
	if err != nil {
		return nil, fmt.Errorf("secrets: %w", ErrMaterial)
	}
	block, err := aes.NewCipher(raw)
	if err != nil {
		return nil, fmt.Errorf("secrets: %d bytes, not the %d an AES-256 key is: %w", len(raw), keyBytes, ErrMaterial)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secrets: %w", err)
	}
	return &Box{gcm: gcm}, nil
}

// Seal wraps plaintext for aad into version ‖ nonce ‖ ciphertext.
//
// Empty plaintext is refused with ErrSealed rather than sealed: an envelope around
// nothing is an oracle for "is this secret empty", and a column full of ciphertexts
// that stand for nothing. A nonce is drawn fresh per call, so two seals of one
// plaintext are different bytes — which is why Seal is deliberately not idempotent
// and a caller that wants a natural key should be comparing versions instead.
func (b *Box) Seal(aad, plaintext []byte) ([]byte, error) {
	if len(plaintext) == 0 {
		return nil, errors.New("secrets: sealing nothing")
	}
	nonce := make([]byte, b.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("secrets: draw a nonce: %w", err)
	}
	// The nonce leads the ciphertext because a nonce means nothing apart from the
	// bytes beside it; the version byte leads that, and Seal appends to both rather
	// than building the envelope twice.
	return b.gcm.Seal(append([]byte{version}, nonce...), nonce, plaintext, aad), nil
}

// Open is the other half. The version byte is checked, a seal this package did not
// write is refused with ErrSealed, and a seal made under another key or for another
// aad refuses the same way: the authentication tag is the answer to "was this mine?",
// and this package has no better one.
func (b *Box) Open(aad, sealed []byte) ([]byte, error) {
	if len(sealed) < 1+b.gcm.NonceSize()+b.gcm.Overhead() || sealed[0] != version {
		return nil, fmt.Errorf("secrets: %w", ErrEnvelope)
	}
	nonce, ciphertext := sealed[1:1+b.gcm.NonceSize()], sealed[1+b.gcm.NonceSize():]
	plain, err := b.gcm.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		// The tag is the answer to "was this mine, for this address, under this
		// key?", and it says no without saying anything about the plaintext.
		return nil, fmt.Errorf("secrets: %w", ErrEnvelope)
	}
	return plain, nil
}

// NonceBytes is the width of the nonce one seal draws, which a caller that wants to
// reason about a sealed byte's minimum length reads rather than recomputes.
func (b *Box) NonceBytes() int { return b.gcm.NonceSize() }
