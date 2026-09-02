package contracts

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// The argon2id parameters, in one place, as constants rather than configuration.
//
// They are OWASP's: 64 MiB of memory, one pass, four lanes. Memory is the axis
// that costs an attacker with a GPU, which is why it is large and the time cost
// is one — a second pass over the same memory doubles our cost and barely moves
// theirs. Four lanes matches a small server core count.
//
// They are constants because a deployment that lowers them has weakened itself
// and would have no reason to write it down. They are stored in every hash all
// the same, in the PHC encoding below, so raising them later leaves the old
// hashes verifiable: Check reads the parameters out of the hash it is checking.
const (
	argonTime    = 1
	argonMemory  = 64 * 1024 // KiB
	argonThreads = 4
	argonSaltLen = 16
	argonKeyLen  = 32
)

// ErrWeakPassword is a password shorter than MinPasswordLength.
var ErrWeakPassword = fmt.Errorf("a password is at least %d characters", MinPasswordLength)

// HashPassword returns the PHC encoding of password:
//
//	$argon2id$v=19$m=65536,t=1,p=4$<salt>$<key>
//
// The salt is 16 bytes from crypto/rand, so two people with the same password
// have different hashes and a precomputed table is worth nothing.
func HashPassword(password string) (string, error) {
	if len([]rune(password)) < MinPasswordLength {
		return "", ErrWeakPassword
	}
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("user: read a salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version,
		argonMemory, argonTime, argonThreads, b64(salt), b64(key)), nil
}

// CheckPassword reports whether password is this user's.
//
// It recomputes with the parameters the stored hash carries rather than the
// constants above, so a hash written under older parameters still verifies, and
// it compares in constant time, so the number of leading bytes that matched is
// not something a caller can measure.
func (u *User) CheckPassword(password string) bool {
	salt, want, time, memory, threads, err := parsePHC(u.PasswordHash)
	if err != nil {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, time, memory, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// EqualWork does the work CheckPassword would have done, and throws it away.
//
// A login for an address nobody has must cost what a login with a wrong
// password costs. Without this the difference between "no such account" and
// "wrong password" is a stopwatch, which is an account enumeration oracle and
// the first step of a targeted attack. The auth module calls it on the path
// where it found no user.
func EqualWork(password string) {
	_ = argon2.IDKey([]byte(password), dummySalt, argonTime, argonMemory, argonThreads, argonKeyLen)
}

// dummySalt is a fixed salt for EqualWork. It is fixed on purpose: nothing is
// stored, nothing is compared, and reading 16 bytes of randomness to throw them
// away is work that is not the work being imitated.
var dummySalt = []byte("platformkit-none")

// parsePHC reads the encoding HashPassword writes. Anything else — an empty
// hash, a bcrypt one, a truncated one — is an error, and CheckPassword answers
// no rather than guessing.
func parsePHC(hash string) (salt, key []byte, time, memory uint32, threads uint8, err error) {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return nil, nil, 0, 0, 0, errors.New("user: not an argon2id hash")
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return nil, nil, 0, 0, 0, errors.New("user: unknown argon2 version")
	}
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads); err != nil {
		return nil, nil, 0, 0, 0, errors.New("user: unreadable argon2 parameters")
	}
	if salt, err = base64.RawStdEncoding.Strict().DecodeString(parts[4]); err != nil {
		return nil, nil, 0, 0, 0, errors.New("user: unreadable salt")
	}
	if key, err = base64.RawStdEncoding.Strict().DecodeString(parts[5]); err != nil {
		return nil, nil, 0, 0, 0, errors.New("user: unreadable key")
	}
	return salt, key, time, memory, threads, nil
}

func b64(b []byte) string { return base64.RawStdEncoding.EncodeToString(b) }
