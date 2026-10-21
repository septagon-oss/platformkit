package internal

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/modules/decide/contracts"
)

type signer struct{ key []byte }

func (s signer) sign(purpose, payload string) string {
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte(purpose + "\x00" + payload))
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (s signer) verify(purpose, token string, fields int) ([]string, bool) {
	if len(token) > 1024 {
		return nil, false
	}
	payload, encodedMAC, ok := strings.Cut(token, ".")
	if !ok {
		return nil, false
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(payload)
	if err != nil || base64.RawURLEncoding.EncodeToString(raw) != payload {
		return nil, false
	}
	mac, err := base64.RawURLEncoding.Strict().DecodeString(encodedMAC)
	if err != nil || len(mac) != sha256.Size || base64.RawURLEncoding.EncodeToString(mac) != encodedMAC {
		return nil, false
	}
	want := hmac.New(sha256.New, s.key)
	want.Write([]byte(purpose + "\x00" + string(raw)))
	parts := strings.Split(string(raw), "/")
	return parts, hmac.Equal(mac, want.Sum(nil)) && len(parts) == fields && parts[0] == "v1"
}

func canonicalID(s string) (uuid.UUID, bool) {
	id, err := uuid.Parse(s)
	return id, err == nil && id != uuid.Nil && id.String() == s
}

func positive(s string) (int64, bool) {
	n, err := strconv.ParseInt(s, 10, 64)
	return n, err == nil && n > 0 && strconv.FormatInt(n, 10) == s
}

func (s signer) invite(c contracts.InviteClaims) string {
	return s.sign("decide.join", strings.Join([]string{"v1", c.TenantID.String(), c.QuestionID.String(),
		strconv.FormatInt(c.Generation, 10), strconv.FormatInt(c.ExpiresAt.Unix(), 10)}, "/"))
}

func (s signer) readInvite(token string) (contracts.InviteClaims, error) {
	p, ok := s.verify("decide.join", token, 5)
	if !ok {
		return contracts.InviteClaims{}, contracts.ErrInvite
	}
	tenant, a := canonicalID(p[1])
	question, b := canonicalID(p[2])
	generation, c := positive(p[3])
	expires, d := positive(p[4])
	if !a || !b || !c || !d {
		return contracts.InviteClaims{}, contracts.ErrInvite
	}
	return contracts.InviteClaims{TenantID: tenant, QuestionID: question, Generation: generation, ExpiresAt: time.Unix(expires, 0).UTC()}, nil
}

func (s signer) undo(c contracts.UndoClaims) string {
	previous := "-"
	if c.Previous != nil {
		previous = string(*c.Previous)
	}
	return s.sign("decide.undo", strings.Join([]string{"v1", c.TenantID.String(), c.QuestionID.String(), c.OptionID.String(), c.PersonID.String(),
		strconv.FormatInt(c.PostRevision, 10), previous, strconv.FormatInt(c.ExpiresAt.Unix(), 10)}, "/"))
}

func (s signer) readUndo(token string) (contracts.UndoClaims, error) {
	p, ok := s.verify("decide.undo", token, 8)
	if !ok {
		return contracts.UndoClaims{}, contracts.ErrUndo
	}
	tenant, a := canonicalID(p[1])
	question, b := canonicalID(p[2])
	option, c := canonicalID(p[3])
	person, d := canonicalID(p[4])
	revision, e := positive(p[5])
	expires, f := positive(p[7])
	var previous *contracts.Value
	switch contracts.Value(p[6]) {
	case contracts.Yes, contracts.Strong, contracts.No, contracts.Skip:
		previous = new(contracts.Value(p[6]))
	case "-":
	default:
		return contracts.UndoClaims{}, contracts.ErrUndo
	}
	if !a || !b || !c || !d || !e || !f {
		return contracts.UndoClaims{}, contracts.ErrUndo
	}
	return contracts.UndoClaims{TenantID: tenant, QuestionID: question, OptionID: option, PersonID: person,
		PostRevision: revision, Previous: previous, ExpiresAt: time.Unix(expires, 0).UTC()}, nil
}
