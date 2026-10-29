// Package openbao is the store behind the port for a deployment that runs a
// secret service: OpenBao, the open fork of Vault, through its HTTP API — KV v2
// for the bytes and the transit engine for the envelope.
//
// It speaks plain net/http rather than the vendor SDK: three JSON calls are all
// this adapter needs, and the SDK's two hundred and forty extra packages are what
// the package budget prices, though the licence rule would allow them.
//
// Two shapes come from the server: the version token is KV v2's own integer,
// spelled "v<N>" so a token of one store never equals one of another, and the
// envelope is transit's vault:vN:… returned verbatim, so a ciphertext written
// before a rotation still reads as it was written. A read is scoped three times —
// kit/secrets' tenant check, KeyAddress, and transit's key derivation — and
// kit/secrets/README.md's Limits reason about each binding.
package openbao

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/secrets"
)

// requestBudget bounds one call. It is longer than kit/cache's 250 ms because a
// transit encrypt is a cryptographic operation on the server rather than a hash
// lookup, and shorter than any caller's patience: a secret service that has not
// answered in three seconds is not going to answer this request, and the caller
// needs an error that is not ErrNotFound in order to stop serving.
const requestBudget = 3 * time.Second

// valueField is the one key this port reads a secret out of. A name holding several
// fields is an operator's own convention and not the port's: a secret here is bytes
// under a name, and a reader that guessed which of three fields was "the" password
// would be a reader with an opinion in it.
const valueField = "value"

// Defaults for the two mount names, exposed so a composition and the fixtures can
// name what the adapter does when told nothing.
const (
	DefaultKVMount      = "secret"
	DefaultTransitMount = "transit"
)

// New returns the store behind cfg's address.
//
// No context, for the reason file.New gives: a plan value is built in a module's
// wire, which has none. Nothing is dialed here — the first call is the first
// request — so a deployment whose secret service is down boots and then answers an
// error that is not ErrNotFound, which is the honest order: the operator learns it
// from the first refused command rather than from a process that will not start.
func New(cfg config.Secrets) (secrets.Store, error) {
	base := strings.TrimSuffix(cfg.URL, "/")
	if base == "" {
		return nil, errors.New("secrets/openbao: secrets.url is empty; this adapter's whole answer is a server behind an address")
	}
	parsed, err := url.Parse(base)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, fmt.Errorf("secrets/openbao: secrets.url %q is not an http or https address", base)
	}
	if parsed.User != nil {
		return nil, errors.New("secrets/openbao: secrets.url carries credentials; the token belongs in secrets.token")
	}
	if cfg.Token == "" {
		return nil, errors.New("secrets/openbao: secrets.token is empty; the port cannot read the token that opens the port")
	}
	kv, transit := cfg.KVMount, cfg.TransitMount
	if kv == "" {
		kv = DefaultKVMount
	}
	if transit == "" {
		transit = DefaultTransitMount
	}
	return secrets.Trace("openbao", &store{
		base:         base,
		token:        cfg.Token,
		kvMount:      kv,
		transitMount: transit,
		client:       &http.Client{Timeout: requestBudget},
	}), nil
}

type store struct {
	base, token, kvMount, transitMount string
	client                             *http.Client
}

// Get unwraps KV v2's envelope: the response's data holds a metadata object and a
// second data object, which holds the operator's fields.
func (s *store) Get(ctx context.Context, ref secrets.Ref) (secrets.Secret, error) {
	response, err := s.kv(ctx, ref)
	if err != nil {
		return secrets.Secret{}, err
	}
	if response.Data.Metadata.DeletionTime != "" || response.Data.Metadata.Destroyed {
		// A version that is soft-deleted answers 200 with the bytes still in it.
		// Whoever deleted it meant it, so the port calls it what it is: a miss.
		return secrets.Secret{}, notFound(ref)
	}
	raw, ok := response.Data.Data[valueField]
	if !ok {
		return secrets.Secret{}, fmt.Errorf("secrets: reading %s: no %q field under %s: %w",
			ref, valueField, s.kvMount, secrets.ErrNotFound)
	}
	return secrets.Secret{Bytes: []byte(raw), Version: fmt.Sprintf("v%d", response.Data.Metadata.Version)}, nil
}

func (s *store) Version(ctx context.Context, ref secrets.Ref) (string, error) {
	response, err := s.kv(ctx, ref)
	if err != nil {
		return "", err
	}
	if response.Data.Metadata.DeletionTime != "" || response.Data.Metadata.Destroyed {
		return "", notFound(ref)
	}
	return fmt.Sprintf("v%d", response.Data.Metadata.Version), nil
}

func (s *store) Seal(ctx context.Context, ref secrets.Ref, plaintext []byte) ([]byte, error) {
	if err := ref.Check(ctx); err != nil {
		return nil, err
	}
	if len(plaintext) == 0 {
		return nil, fmt.Errorf("secrets: sealing nothing: %w", secrets.ErrSealed)
	}
	body := map[string]string{"plaintext": base64.StdEncoding.EncodeToString(plaintext)}
	if contextValue := deriveContext(ref.Tenant()); contextValue != "" {
		body["context"] = contextValue
	}
	var out struct {
		Data struct {
			Ciphertext string `json:"ciphertext"`
			KeyVersion int    `json:"key_version"`
		} `json:"data"`
	}
	// The transit endpoints take the key's name as the last path segment and answer
	// the envelope in data.ciphertext; key_version is read rather than ignored
	// because a response with no version is a server that did not do the work.
	if err := s.transit(ctx, "encrypt", ref, body, &out); err != nil {
		return nil, err
	}
	if out.Data.Ciphertext == "" || out.Data.KeyVersion == 0 {
		return nil, fmt.Errorf("secrets: sealing under %s: %w", ref, secrets.ErrSealed)
	}
	return []byte(out.Data.Ciphertext), nil
}

func (s *store) Open(ctx context.Context, ref secrets.Ref, sealed []byte) ([]byte, error) {
	if err := ref.Check(ctx); err != nil {
		return nil, err
	}
	if !isTransitText(sealed) {
		return nil, fmt.Errorf("secrets: opening bytes this key service did not write: %w", secrets.ErrSealed)
	}
	body := map[string]string{"ciphertext": string(sealed)}
	if contextValue := deriveContext(ref.Tenant()); contextValue != "" {
		body["context"] = contextValue
	}
	var out struct {
		Data struct {
			Plaintext string `json:"plaintext"`
		} `json:"data"`
	}
	if err := s.transit(ctx, "decrypt", ref, body, &out); err != nil {
		return nil, err
	}
	plain, err := base64.StdEncoding.DecodeString(out.Data.Plaintext)
	if err != nil {
		return nil, fmt.Errorf("secrets: opening under %s: %w", ref, secrets.ErrSealed)
	}
	return plain, nil
}

// kv is one read of the v2 endpoint, with the tenant segment inside the path so
// that a mount policy can be written per customer as well as the port refusing a
// cross-tenant address before it gets here.
func (s *store) kv(ctx context.Context, ref secrets.Ref) (kvResponse, error) {
	if err := ref.Check(ctx); err != nil {
		return kvResponse{}, err
	}
	var response kvResponse
	path := s.kvMount + "/data/" + addressPath(ref)
	if err := s.call(ctx, http.MethodGet, path, nil, &response); err != nil {
		if errors.Is(err, errAbsent) {
			return kvResponse{}, notFound(ref)
		}
		return kvResponse{}, err
	}
	return response, nil
}

// sharedKeyPrefix marks a transit key as the installation's own. It is not a name an
// operator chooses and not part of a name: it is the seam that keeps the two key
// address spaces from spelling each other — see KeyAddress.
const sharedKeyPrefix = "shared-"

// KeyAddress is a key's whole path inside the transit mount: the name under a
// fixed prefix for the installation's own key, and the tenant's UUID as a prefix
// for a customer's.
//
// One segment rather than KV's nested <tenant>/<name>, because the transit mount
// answers 404 for a nested key name before it looks at anything else. The UUID is
// the prefix, in full, so a policy reads transit/encrypt/<tenant>-* and one
// customer cannot address another's key by guessing a suffix.
//
// The shared prefix keeps the two spellings from meeting: a tenant address always
// begins with a canonical UUID, whose first character is a hex digit, and no
// shared address begins with shared-. That holds whatever the mount looks like;
// kit/secrets/README.md's Limits reason about it further.
func KeyAddress(ref secrets.Ref) string {
	if ref.Tenant() == uuid.Nil {
		return sharedKeyPrefix + ref.Name()
	}
	return ref.Tenant().String() + "-" + ref.Name()
}

// transit is one encrypt or decrypt call: the key's name is the last segment, the
// body is JSON, and a 400 is ErrSealed rather than a miss because a ciphertext the
// server cannot open is a ciphertext this port should not have been handed.
func (s *store) transit(ctx context.Context, verb string, ref secrets.Ref, body map[string]string, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("secrets: %s: %w", verb, err)
	}
	// A secret's name and a key's name share this port's address space, and the key
	// lives under its own one-segment address rather than KV's nested one — see
	// KeyAddress. An operator mounts keys under their own names, and nothing about
	// the envelope differs from a read.
	path := s.transitMount + "/" + verb + "/" + KeyAddress(ref)
	if err := s.callStatus(ctx, http.MethodPost, path, payload, out, map[int]error{
		http.StatusBadRequest: fmt.Errorf("secrets: %w", secrets.ErrSealed),
	}); err != nil {
		if errors.Is(err, errAbsent) {
			return notFound(ref)
		}
		if errors.Is(err, secrets.ErrSealed) {
			return fmt.Errorf("secrets: %sing under %s: %w", verb, ref, err)
		}
		return err
	}
	return nil
}

// call is one request and its status map, which is the whole of what this adapter
// knows about the server's health:
//
//	200 with a body          → the answer
//	404                      → ErrNotFound, the store is up and does not have it
//	403                      → a permission refusal naming the mount, never the token
//	400                      → the caller's own refusal, or ErrSealed
//	429, 5xx, transport      → an error that is not ErrNotFound, so a caller that
//	                           fails open on a miss fails closed on an outage
func (s *store) call(ctx context.Context, method, path string, body []byte, out any) error {
	return s.callStatus(ctx, method, path, body, out, nil)
}

func (s *store) callStatus(ctx context.Context, method, path string, body []byte, out any, statuses map[int]error) error {
	request, err := http.NewRequestWithContext(ctx, method, s.base+"/v1/"+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("secrets: %s %s: %w", method, path, err)
	}
	request.Header.Set("X-Vault-Token", s.token)
	request.Header.Set("Content-Type", "application/json")
	response, err := s.client.Do(request)
	if err != nil {
		// The client's own error text quotes the URL, which carries no token — the
		// token rides a header — so it is safe to wrap, and it is the sentence an
		// operator needs: which address did not answer.
		return fmt.Errorf("secrets: the secret service at %s did not answer: %w", describe(s.base), err)
	}
	defer response.Body.Close()
	if err := statusError(response, response.Request.URL.Path, s.base, statuses); err != nil {
		return err
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("secrets: reading the answer from %s: %w", describe(s.base), err)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("secrets: the answer from %s is not the JSON this port reads: %w", describe(s.base), err)
	}
	return nil
}

// errAbsent is the server's 404, before the sentence a caller has to make about
// which address was absent. It is unexported because nothing outside this package
// branches on it: what a caller gets is ErrNotFound, and what a caller gets for
// every other failure is an error that is not ErrNotFound.
var errAbsent = errors.New("openbao: no such path")

// statusError turns a response's status into the one answer that is true of it. The
// body is never quoted, and neither is the token: an error body from a secret
// service may repeat whatever it was given, and this request's credential rides a
// header this package never echoes.
//
// 403 is refused as a refusal and not as a miss. A mount policy that says no is not
// "the secret does not exist" — an adapter that answered ErrNotFound for it would
// let a caller that fails open on a miss serve a request whose credential it never
// read, and would send an operator to mount a secret that is already mounted.
func statusError(response *http.Response, path, base string, statuses map[int]error) error {
	switch {
	case response.StatusCode == http.StatusNotFound:
		return errAbsent
	case response.StatusCode == http.StatusForbidden:
		return fmt.Errorf("secrets: the token at %s is not allowed on %s", describe(base), path)
	case response.StatusCode == http.StatusUnauthorized:
		return fmt.Errorf("secrets: the token at %s is not a token this server accepts", describe(base))
	case response.StatusCode >= 400:
		if refusal, ok := statuses[response.StatusCode]; ok {
			return refusal
		}
		return fmt.Errorf("secrets: the secret service at %s answered %s", describe(base), response.Status)
	}
	return nil
}

// notFound is a miss with the address in the sentence and the sentinel reachable by
// errors.Is, which is the only part of an error a caller is allowed to branch on.
func notFound(ref secrets.Ref) error {
	return fmt.Errorf("secrets: reading %s: %w", ref, secrets.ErrNotFound)
}

// addressPath is the address as a KV path: the tenant's UUID as the first segment of
// a two-segment name, so a policy may be written for `secret/data/<tenant>/*` and a
// mount's own listing of one customer shows nothing of another.
func addressPath(ref secrets.Ref) string {
	if ref.Tenant() == uuid.Nil {
		return ref.Name()
	}
	return ref.Tenant().String() + "/" + ref.Name()
}

// deriveContext is the transit key-derivation context: the tenant's own 16 bytes,
// base64'd, and "" — no context at all — for the installation's own key.
//
// Omitted rather than sent empty, because a derived key refuses a call that carries
// no context and that refusal is the deployment's answer to "may the installation's
// secrets share a key with a customer's": the two spellings are how one server keeps
// two keys apart, and this port only carries the question it was mounted to answer.
func deriveContext(tenant uuid.UUID) string {
	if tenant == uuid.Nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(tenant[:])
}

// isTransitText is the shape a transit ciphertext has: `vault:v<N>:…`. Bytes that
// are not that are not this key service's output — a local box seal, a ciphertext
// from another deployment, or a column that holds a plaintext — and refusing them by
// shape is what keeps Open from POSTing somebody's data as a ciphertext.
func isTransitText(sealed []byte) bool {
	// A base64 alphabet holds no colon, so a well-formed ciphertext is exactly
	// `vault`, `v<N>`, and the payload: three colon-separated parts, and anything
	// else arrived from somewhere else.
	parts := strings.Split(string(sealed), ":")
	if len(parts) != 3 || parts[0] != "vault" || parts[2] == "" {
		return false
	}
	digits := strings.TrimPrefix(parts[1], "v")
	if digits == "" || len(digits) == len(parts[1]) {
		return false
	}
	for i := 0; i < len(digits); i++ {
		if digits[i] < '0' || digits[i] > '9' {
			return false
		}
	}
	return true
}

// describe is an address as this package may write it: scheme, host and port, no
// userinfo and no path. kit/config refuses credentials in secrets.url, and New is an
// exported constructor a composition may call with a config it built itself, so the
// adapter is where the sentence gets scrubbed rather than where the refusal trusts
// someone else to have checked.
func describe(base string) string {
	parsed, err := url.Parse(base)
	if err != nil || parsed.Host == "" {
		return "(an address this package cannot read)"
	}
	parsed.User, parsed.RawQuery, parsed.RawPath, parsed.Fragment = nil, "", "", ""
	return parsed.String()
}

// kvResponse is KV v2's read shape, with the one field of the metadata this port
// needs: its version, and whether a deletion is pending.
type kvResponse struct {
	Data struct {
		Data     map[string]string `json:"data"`
		Metadata struct {
			Version      int    `json:"version"`
			DeletionTime string `json:"deletion_time"`
			Destroyed    bool   `json:"destroyed"`
		} `json:"metadata"`
	} `json:"data"`
}
