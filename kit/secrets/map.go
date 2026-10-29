package secrets

import (
	"context"
	"fmt"
)

// Map returns the store an in-process map is: the composition of a test, and
// the double every module's own test runs on so a test of a reader never has to
// import an adapter.
//
// It is not a fourth adapter and not a cache — it is what kit/cache's
// in-process store is for that port. The conformance suite runs over it first,
// so a green local run is evidence about the port rather than theatre about a
// map. The two arguments are the caller's own maps, kept by reference: putting
// a secret in after the store is built is a rotation, and two handles over one
// pair of maps are two replicas of one store, which is what the suite's sharing
// cases ask of an adapter.
//
// Key material is spelled as in the file and env stores — base64 of exactly 32
// bytes — so a wrong width is refused in the deployment's own sentence.
func Map(secrets map[Ref]Secret, keys map[Ref][]byte) Store {
	return Trace("map", &inMemory{secrets: secrets, keys: keys})
}

// Broken answers err for every method, and is the double of a store that cannot be
// reached.
//
// A caller that fails open when a secret is missing needs to be tested against an
// outage as well as against a miss, and the two look identical unless the test can
// put one on the table: an error that is not ErrNotFound is the outage's shape, and
// the conformance suite refuses an adapter that answers one for the other.
func Broken(err error) Store { return Trace("broken", unreachable{err: err}) }

// inMemory is Map's body. It holds no lock, which is the same promise a map makes:
// a test that rotates a secret from another goroutine owns that race.
type inMemory struct {
	secrets map[Ref]Secret
	keys    map[Ref][]byte
}

func (m *inMemory) Get(ctx context.Context, ref Ref) (Secret, error) {
	if err := ref.Check(ctx); err != nil {
		return Secret{}, err
	}
	secret, ok := m.secrets[ref]
	if !ok {
		return Secret{}, fmt.Errorf("secrets: reading %s: %w", ref, ErrNotFound)
	}
	return Secret{Bytes: append([]byte(nil), secret.Bytes...), Version: secret.Version}, nil
}

func (m *inMemory) Version(ctx context.Context, ref Ref) (string, error) {
	if err := ref.Check(ctx); err != nil {
		return "", err
	}
	secret, ok := m.secrets[ref]
	if !ok {
		return "", fmt.Errorf("secrets: reading %s: %w", ref, ErrNotFound)
	}
	return secret.Version, nil
}

func (m *inMemory) Seal(ctx context.Context, ref Ref, plaintext []byte) ([]byte, error) {
	material, err := m.key(ctx, ref)
	if err != nil {
		return nil, err
	}
	return SealWith(ref, material, plaintext)
}

func (m *inMemory) Open(ctx context.Context, ref Ref, sealed []byte) ([]byte, error) {
	material, err := m.key(ctx, ref)
	if err != nil {
		return nil, err
	}
	return OpenWith(ref, material, sealed)
}

func (m *inMemory) key(ctx context.Context, ref Ref) ([]byte, error) {
	if err := ref.Check(ctx); err != nil {
		return nil, err
	}
	material, ok := m.keys[ref]
	if !ok {
		return nil, fmt.Errorf("secrets: sealing under %s: %w", ref, ErrNotFound)
	}
	return material, nil
}

// unreachable is Broken's body: a store whose every answer is the failure it was
// built with, and which reaches no address to produce it.
type unreachable struct{ err error }

func (u unreachable) Get(context.Context, Ref) (Secret, error) { return Secret{}, u.err }
func (u unreachable) Version(context.Context, Ref) (string, error) {
	return "", u.err
}
func (u unreachable) Seal(context.Context, Ref, []byte) ([]byte, error) { return nil, u.err }
func (u unreachable) Open(context.Context, Ref, []byte) ([]byte, error) { return nil, u.err }
