package secrets

import (
	"errors"
	"fmt"

	"github.com/septagon-oss/platformkit/kit/secrets/internal/box"
)

// SealWith is the envelope for a store that holds the key material itself — the
// environment, a mounted file, a test's map — rather than a service that keeps keys
// and never shows them.
//
// It exists so the local answer is written once: the material is base64 of exactly
// 32 bytes, the address is the additional authenticated data, the nonce is drawn per
// call and the bytes carry one version byte. An adapter that got any of that wrong
// would produce ciphertexts its own next release could not open, which is the
// expensive class of bug, so it is done here and every local store calls it. A store
// that runs a key service (OpenBao's transit, a cloud KMS) does not: a crypto path in
// front of a key store would be a second answer to which key a ciphertext sits under.
//
// Key material of the wrong width, and ciphertext these bytes did not come from, are
// ErrSealed with the address in the sentence: both are defects rather than
// conditions, and neither is answered with a guess at what was inside.
func SealWith(ref Ref, material, plaintext []byte) ([]byte, error) {
	if len(plaintext) == 0 {
		return nil, fmt.Errorf("secrets: sealing nothing under %s: %w", ref, ErrSealed)
	}
	key, err := box.Key(material)
	if err != nil {
		return nil, fmt.Errorf("secrets: sealing under %s: %w", ref, refused(err))
	}
	sealed, err := key.Seal(ref.AAD(), plaintext)
	if err != nil {
		return nil, fmt.Errorf("secrets: sealing under %s: %w", ref, refused(err))
	}
	return sealed, nil
}

// OpenWith is SealWith's other half, under the same address.
func OpenWith(ref Ref, material, sealed []byte) ([]byte, error) {
	key, err := box.Key(material)
	if err != nil {
		return nil, fmt.Errorf("secrets: opening under %s: %w", ref, refused(err))
	}
	plain, err := key.Open(ref.AAD(), sealed)
	if err != nil {
		return nil, fmt.Errorf("secrets: opening under %s: %w", ref, refused(err))
	}
	return plain, nil
}

// refused is box's own refusal wearing the port's sentinel, so a caller that
// branches on errors.Is has one name for "these bytes are not mine" whichever store
// answered. box's sentence survives in the chain for whoever reads the log line.
func refused(err error) error {
	if errors.Is(err, box.ErrMaterial) || errors.Is(err, box.ErrEnvelope) {
		return fmt.Errorf("%w: %w", ErrSealed, err)
	}
	return err
}
