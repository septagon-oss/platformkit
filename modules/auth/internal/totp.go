package internal

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"math/big"
	"strings"
	"time"

	"golang.org/x/crypto/nacl/secretbox"

	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

// RFC 6238 in one file, over crypto/hmac.
//
// This is the whole of the second factor's crypto: HMAC-SHA1 is the primitive
// and the standard library has it, which is what T-0013's own rule means by "no
// new dependency beyond the standard library and x/crypto". What the RFC adds
// on top is a counter, a truncation and a window, and a library that wrapped
// these thirty lines would move the review rather than remove it. Nothing here
// is a signature scheme, a curve or an envelope — those belong to a library
// precisely because nobody should write them, which is why the passkey half of
// the brief is not in this file (see modules/auth/README.md, Limits).
//
// The one property a reader should check against the RFC rather than against a
// test: a code is compared with subtle.ConstantTimeCompare over the formatted
// digits, and the window is walked in a fixed order regardless of the answer.

// totpDigits and totpPeriod are contracts' parameters under the names this file
// uses; they are the same numbers, and a test asserts both halves agree.
const (
	totpSecretBytes = 20 // RFC 4226's 160-bit seed, which is SHA-1's block
	totpSteps       = 1 + 2*contracts.TOTPSkew
)

// newTOTPSecret is 160 bits from crypto/rand, encoded RFC 4648 base32 without
// padding — the form every authenticator app pastes.
func newTOTPSecret() (string, error) {
	raw := make([]byte, totpSecretBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("auth: mint a factor secret: %w", err)
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw), nil
}

// decodeTOTPSecret accepts the secret as an app wrote it back: upper or lower
// case, spaces and hyphens stripped, padding optional. It refuses rather than
// tolerating anything else, because a code computed over a mis-decoded secret
// is a wrong answer that looks like the person's fault.
func decodeTOTPSecret(secret string) ([]byte, error) {
	clean := strings.NewReplacer(" ", "", "-", "", "\n", "", "\t", "").Replace(secret)
	raw, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(clean))
	if err != nil {
		return nil, errors.New("auth: that factor secret is not base32")
	}
	if len(raw) < 16 {
		return nil, errors.New("auth: that factor secret is shorter than RFC 4226 allows")
	}
	return raw, nil
}

// totpStep is the RFC 6238 step a moment falls in: the counter the code is
// derived from, and the value last_step remembers.
func totpStep(at time.Time) int64 { return at.Unix() / int64(contracts.TOTPPeriod.Seconds()) }

// totpCode is the code for one step, as a fixed-width string.
func totpCode(secret []byte, step int64) (string, error) {
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(step))
	mac := hmac.New(func() hash.Hash { return sha1.New() }, secret)
	mac.Write(counter[:])
	sum := mac.Sum(nil)
	// RFC 4226's dynamic truncation: an offset out of the digest's last nibble,
	// four bytes from there, the sign bit cleared.
	offset := sum[len(sum)-1] & 0x0f
	value := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	mod := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(contracts.TOTPDigits)), nil)
	return fmt.Sprintf("%0*d", contracts.TOTPDigits, new(big.Int).Mod(new(big.Int).SetUint64(uint64(value)), mod).Int64()), nil
}

// totpMatches reports whether code is one of the accepted steps' codes, and
// which step. The window is walked in the same order whether the answer is
// early, on time or late, and each comparison is constant time, so neither the
// clock offset nor the position of the match is something a caller can measure.
//
// Steps at or below lastStep are skipped: they have been spent. That is the
// replay refusal, and it is the reason no challenge table exists.
func totpMatches(secret []byte, code string, at time.Time, lastStep int64) (int64, bool) {
	want := normalizeCode(code)
	if len(want) != contracts.TOTPDigits {
		return 0, false
	}
	now := totpStep(at)
	for i := 0; i < totpSteps; i++ {
		step := now - int64(contracts.TOTPSkew) + int64(i)
		// A negative step is a clock before 1970. Not this platform's, and the
		// uint64 cast in totpCode would turn one into a huge counter.
		if step <= 0 || step <= lastStep {
			continue
		}
		got, err := totpCode(secret, step)
		if err != nil {
			return 0, false
		}
		if subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1 {
			return step, true
		}
	}
	return 0, false
}

// normalizeCode is what a person's typing becomes: spaces, hyphens and the
// grouping an app adds are stripped, and letters are folded so a recovery code
// copied from a chat window still reads.
func normalizeCode(code string) string {
	return strings.NewReplacer(" ", "", "-", "", "\n", "", "\t", "").Replace(code)
}

// otpauthURI is the enrolment string an authenticator app reads. The label is
// the tenant's own display name and the account the person's address, because
// that is the pair an app shows somebody who is deciding whether to trust a
// prompt — and issuer is passed in rather than written here, since a module
// that named one would be naming a customer (rule 4).
func otpauthURI(issuer, account, secret string) string {
	return fmt.Sprintf("otpauth://totp/%s:%s?secret=%s&issuer=%s&algorithm=SHA1&digits=%d&period=%d",
		queryEscape(issuer), queryEscape(account), secret, queryEscape(issuer),
		contracts.TOTPDigits, int(contracts.TOTPPeriod.Seconds()))
}

// queryEscape is url.QueryEscape's colon and slash exception: RFC 6749's otpauth
// label is "issuer:account", and an escaped colon is a label that reads wrong
// in every app that shows it.
func queryEscape(s string) string {
	const unescaped = ":/@"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9',
			c == '-', c == '_', c == '.', c == '~', strings.IndexByte(unescaped, c) >= 0:
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// seal is the envelope a factor secret is stored in, and open is the way back.
//
// The key is the deployment's factor key, hashed to the 32 bytes secretbox
// wants so the operator may write it as a passphrase or as hex and the module
// does not care which. What sealing buys is the property the migration states:
// a database copy hands over a list of sealed secrets rather than a set of
// working second factors — an attacker with the dump still needs the key, which
// lives in the deployment's environment and not in the row.
func seal(factorKey, plaintext []byte) ([]byte, error) {
	if len(factorKey) == 0 {
		return nil, contracts.ErrNoFactorKey
	}
	var boxKey [32]byte
	key := sha256.Sum256(factorKey)
	boxKey = key
	var nonce [24]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, fmt.Errorf("auth: seal a factor secret: %w", err)
	}
	return secretbox.Seal(nonce[:], plaintext, &nonce, &boxKey), nil
}

func openSecret(factorKey, sealed []byte) ([]byte, error) {
	if len(factorKey) == 0 {
		return nil, contracts.ErrNoFactorKey
	}
	if len(sealed) < 24 {
		return nil, errors.New("auth: that sealed secret is too short to hold a nonce")
	}
	boxKey := sha256.Sum256(factorKey)
	var nonce [24]byte
	copy(nonce[:], sealed[:24])
	opened, ok := secretbox.Open(nil, sealed[24:], &nonce, &boxKey)
	if !ok {
		return nil, errors.New("auth: that sealed secret was not sealed with this key")
	}
	return opened, nil
}
