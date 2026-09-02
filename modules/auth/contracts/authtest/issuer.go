package authtest

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"

	jose "github.com/go-jose/go-jose/v4"
)

// Issuer is an OpenID Connect provider a test can sign in against: discovery,
// a JWKS, and a token endpoint that returns a signed id token for a code the
// test handed out.
//
// There is no authorization endpoint, because a browser is the thing that would
// use it: a test follows the redirect itself, reads the state out of it, and
// calls the callback with a code it registered here. That is the whole round
// trip the application takes part in.
type Issuer struct {
	*httptest.Server

	key *rsa.PrivateKey

	mu     sync.Mutex
	claims map[string]map[string]any
}

// NewIssuer starts one. It is closed when the test ends.
func NewIssuer(t interface {
	Fatalf(string, ...any)
	Cleanup(func())
},
) *Issuer {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("authtest: generate a signing key: %v", err)
	}
	i := &Issuer{key: key, claims: map[string]map[string]any{}}
	i.Server = httptest.NewServer(http.HandlerFunc(i.serve))
	t.Cleanup(i.Server.Close)
	return i
}

// Issue registers a code and the claims the token endpoint will return for it.
// A test names an address and whether the provider says it verified it, because
// that second answer is what the application refuses on.
func (i *Issuer) Issue(code, email string, verified bool, audience, nonce string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.claims[code] = map[string]any{
		"iss": i.URL, "sub": "subject-" + email, "aud": audience,
		"exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(),
		"nonce": nonce, "email": email, "email_verified": verified,
	}
}

func (i *Issuer) serve(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/.well-known/openid-configuration":
		write(w, map[string]any{
			"issuer":                                i.URL,
			"authorization_endpoint":                i.URL + "/authorize",
			"token_endpoint":                        i.URL + "/token",
			"jwks_uri":                              i.URL + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
			"response_types_supported":              []string{"code"},
			"subject_types_supported":               []string{"public"},
		})
	case "/jwks":
		write(w, jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
			Key: i.key.Public(), KeyID: "test", Algorithm: "RS256", Use: "sig",
		}}})
	case "/token":
		_ = r.ParseForm()
		i.mu.Lock()
		claims, ok := i.claims[r.Form.Get("code")]
		i.mu.Unlock()
		if !ok {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}
		write(w, map[string]any{
			"access_token": "opaque", "token_type": "Bearer", "expires_in": 3600,
			"id_token": i.sign(claims),
		})
	default:
		http.NotFound(w, r)
	}
}

// sign is an RS256 JWT over the claims, which is the one thing a test issuer
// has to do for real: the application verifies the signature against the JWKS.
func (i *Issuer) sign(claims map[string]any) string {
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: i.key},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "test"))
	if err != nil {
		panic("authtest: " + err.Error())
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		panic("authtest: " + err.Error())
	}
	signed, err := signer.Sign(payload)
	if err != nil {
		panic("authtest: " + err.Error())
	}
	raw, err := signed.CompactSerialize()
	if err != nil {
		panic("authtest: " + err.Error())
	}
	return raw
}

func write(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
