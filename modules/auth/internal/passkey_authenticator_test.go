package internal_test

// softAuthenticator is a platform authenticator written out from the WebAuthn
// Level 3 data layout (§6.1 authenticator data, §6.5.4 the "none" attestation
// statement, RFC 9053's EC2 COSE key) rather than borrowed from the library's
// ceremony code — only its CBOR encoder writes the bytes: one ES256 key, one
// credential id, a counter the case sets, and the two JSON bodies a browser hands back from navigator.credentials.create and .get.

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/go-webauthn/webauthn/protocol/webauthncbor"
	"github.com/google/uuid"
)

type softAuthenticator struct {
	key     *ecdsa.PrivateKey
	id      []byte
	user    []byte
	rpID    string
	origin  string
	counter uint32
}

func newSoftAuthenticator(t *testing.T, rpID string) *softAuthenticator {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("make a passkey: %v", err)
	}
	id := make([]byte, 32)
	_, _ = rand.Read(id)
	return &softAuthenticator{key: key, id: id, rpID: rpID, origin: "http://" + rpID}
}

var b64 = base64.RawURLEncoding

// challengeOf is the challenge a begin leg handed the browser.
func challengeOf(t *testing.T, body []byte) (uuid.UUID, string) {
	t.Helper()
	var out struct {
		Ceremony uuid.UUID `json:"ceremony"`
		Options  struct {
			PublicKey struct {
				Challenge string `json:"challenge"`
				User      struct {
					ID string `json:"id"`
				} `json:"user"`
			} `json:"publicKey"`
		} `json:"options"`
	}
	if err := json.Unmarshal(body, &out); err != nil || out.Options.PublicKey.Challenge == "" {
		t.Fatalf("a begin leg answered %s, want a ceremony and a publicKey challenge (%v)", body, err)
	}
	return out.Ceremony, out.Options.PublicKey.Challenge
}

func (a *softAuthenticator) clientData(kind, challenge string) []byte {
	cd, _ := json.Marshal(map[string]any{
		"type": kind, "challenge": challenge, "origin": a.origin, "crossOrigin": false,
	})
	return cd
}

// authData is rpIdHash ‖ flags ‖ signCount, with the attested credential data
// appended when attested is true. Flags: UP and UV, plus AT when attested.
func (a *softAuthenticator) authData(attested bool) []byte {
	rp := sha256.Sum256([]byte(a.rpID))
	out := append([]byte{}, rp[:]...)
	flags := byte(0x01 | 0x04)
	if attested {
		flags |= 0x40
	}
	out = append(out, flags)
	out = binary.BigEndian.AppendUint32(out, a.counter)
	if !attested {
		return out
	}
	out = append(out, make([]byte, 16)...) // AAGUID: none
	out = binary.BigEndian.AppendUint16(out, uint16(len(a.id)))
	out = append(out, a.id...)
	point, err := a.key.PublicKey.Bytes() // 0x04 ‖ X ‖ Y
	if err != nil {
		panic(err)
	}
	x, y := point[1:33], point[33:]
	cose, err := webauthncbor.Marshal(map[int]any{1: 2, 3: -7, -1: 1, -2: x, -3: y})
	if err != nil {
		panic(err)
	}
	return append(out, cose...)
}

// created is the body navigator.credentials.create hands back for challenge.
func (a *softAuthenticator) created(t *testing.T, challenge string) json.RawMessage {
	t.Helper()
	att, err := webauthncbor.Marshal(map[string]any{
		"fmt": "none", "attStmt": map[string]any{}, "authData": a.authData(true),
	})
	if err != nil {
		t.Fatalf("encode an attestation: %v", err)
	}
	body, _ := json.Marshal(map[string]any{
		"id": b64.EncodeToString(a.id), "rawId": b64.EncodeToString(a.id), "type": "public-key",
		"response": map[string]any{
			"clientDataJSON":    b64.EncodeToString(a.clientData("webauthn.create", challenge)),
			"attestationObject": b64.EncodeToString(att),
		},
	})
	return body
}

// asserted is the body navigator.credentials.get hands back for challenge,
// signed over authenticatorData ‖ SHA-256(clientDataJSON) with the user handle
// the credential was created with.
func (a *softAuthenticator) asserted(t *testing.T, challenge string) json.RawMessage {
	t.Helper()
	cd := a.clientData("webauthn.get", challenge)
	ad := a.authData(false)
	digest := sha256.Sum256(cd)
	signed := sha256.Sum256(append(append([]byte{}, ad...), digest[:]...))
	sig, err := ecdsa.SignASN1(rand.Reader, a.key, signed[:])
	if err != nil {
		t.Fatalf("sign an assertion: %v", err)
	}
	body, _ := json.Marshal(map[string]any{
		"id": b64.EncodeToString(a.id), "rawId": b64.EncodeToString(a.id), "type": "public-key",
		"response": map[string]any{
			"clientDataJSON":    b64.EncodeToString(cd),
			"authenticatorData": b64.EncodeToString(ad),
			"signature":         b64.EncodeToString(sig),
			"userHandle":        b64.EncodeToString(a.user),
		},
	})
	return body
}

// enrolPasskey runs both enrolment legs for the person holding session and
// reports what the finish leg answered.
func enrolPasskey(t *testing.T, router http.Handler, session string, a *softAuthenticator) (int, string) {
	t.Helper()
	return enrolPasskeyNamed(t, router, session, a, "laptop")
}

// enrolPasskeyNamed is enrolPasskey with the name its owner typed.
func enrolPasskeyNamed(t *testing.T, router http.Handler, session string, a *softAuthenticator, name string) (int, string) {
	t.Helper()
	res := call(t, router, http.MethodPost, "/api/v1/auth/factors/passkey/begin", "", withSession(session))
	if res.Code != http.StatusOK {
		t.Fatalf("begin enrolling a passkey = %d %s", res.Code, res.Body.String())
	}
	ceremony, challenge := challengeOf(t, res.Body.Bytes())
	var opts struct {
		Options struct {
			PublicKey struct {
				User struct {
					ID string `json:"id"`
				} `json:"user"`
			} `json:"publicKey"`
		} `json:"options"`
	}
	_ = json.Unmarshal(res.Body.Bytes(), &opts)
	user, err := b64.DecodeString(opts.Options.PublicKey.User.ID)
	if err != nil || len(user) == 0 {
		t.Fatalf("the enrolment options name no user handle: %s", res.Body.String())
	}
	a.user = user
	body, _ := json.Marshal(map[string]any{
		"ceremony": ceremony, "response": a.created(t, challenge), "name": name,
	})
	res = call(t, router, http.MethodPost, "/api/v1/auth/factors/passkey/finish", string(body), withSession(session))
	return res.Code, res.Body.String()
}
