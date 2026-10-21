package internal

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/modules/decide/contracts"
)

func TestCredentialsRequireCanonicalClaimsAndTheirOwnPurpose(t *testing.T) {
	s := signer{key: []byte("isolated-credential-test-key-32-bytes")}
	claim := contracts.InviteClaims{TenantID: uuid.New(), QuestionID: uuid.New(), Generation: 1, ExpiresAt: time.Unix(1900000000, 0).UTC()}
	token := s.invite(claim)
	got, err := s.readInvite(token)
	if err != nil || got != claim {
		t.Fatalf("round trip: %+v %v", got, err)
	}
	if _, err := s.readUndo(token); err == nil {
		t.Fatal("join credential accepted as rewind")
	}
	payload := "v1/" + claim.TenantID.String() + "/" + claim.QuestionID.String() + "/1/1900000000"
	for _, malformed := range []string{strings.Replace(payload, "/1/", "/01/", 1), strings.Replace(payload, "/1/", "/0/", 1), payload + "/extra", strings.ToUpper(payload), strings.Replace(payload, "/1900000000", "/01900000000", 1)} {
		if _, err := s.readInvite(s.sign("decide.join", malformed)); err == nil {
			t.Fatalf("noncanonical claims accepted: %s", malformed)
		}
	}
	for _, malformed := range []string{token + "=", strings.Repeat("a", 1025), base64.RawURLEncoding.EncodeToString([]byte(payload)) + ".AA", "", "."} {
		if _, err := s.readInvite(malformed); err == nil {
			t.Fatal("malformed framing accepted")
		}
	}
	undo := contracts.UndoClaims{TenantID: claim.TenantID, QuestionID: claim.QuestionID, OptionID: uuid.New(), PersonID: uuid.New(), PostRevision: 2, Previous: new(contracts.Strong), ExpiresAt: claim.ExpiresAt}
	decoded, err := s.readUndo(s.undo(undo))
	if err != nil || decoded.PostRevision != 2 || decoded.Previous == nil || *decoded.Previous != contracts.Strong || decoded.PersonID != undo.PersonID {
		t.Fatalf("undo: %+v %v", decoded, err)
	}
}
