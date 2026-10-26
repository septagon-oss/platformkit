package secrets

import (
	"errors"
	"fmt"

	"github.com/septagon-oss/platformkit/kit/secrets/internal/box"
)

// SealWith is the envelope for a store that holds the key material itself — the
// environment, a mounted file, a test's map — rather than a service that keeps
// keys and never shows them.
//
// The local answer is written once so it cannot be got wrong twelve ways: the
// material is base64 of exactly 32 bytes, the address is the additional
// authenticated data, the nonce is drawn per call and the bytes carry one
// version byte. A store that runs a key service takes no crypto path in front
// of it — which key a ciphertext sits under stays that service's own answer.
//
// Key material of the wrong width, and ciphertext these bytes did not come
// from, are ErrSealed with the address in the sentence: defects, not
// conditions, and never answered with a guess at what was inside.
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
