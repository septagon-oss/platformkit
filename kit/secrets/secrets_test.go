package secrets_test

import (
	"errors"
	"testing"

	"strconv"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/secrets"
	"github.com/septagon-oss/platformkit/kit/secrets/secretstest"
)

// TestTheInProcessStoreConforms runs the suite every adapter runs over the store a
// test builds itself. It is the case that makes a green local run evidence: the map
// is not a second, gentler port, and a decision the map gets wrong is a decision
// visible without a container.
// errStoreDown is what a store that stopped answering says, in the shape a caller
// must be able to tell apart from ErrNotFound.
var errStoreDown = errors.New("the secret service is not answering")

// writes counts a test's writes per address, so the in-process store's token moves
// when the bytes do and only then.
var writes = map[secrets.Ref]int{}

func TestTheInProcessStoreConforms(t *testing.T) {
	secretsMap := map[secrets.Ref]secrets.Secret{}
	keys := map[secrets.Ref][]byte{}
	adapter := secretstest.Adapter{
		Name:  "map",
		Store: func(*testing.T) secrets.Store { return secrets.Map(secretsMap, keys) },
		Failing: func(*testing.T) secrets.Store {
			return secrets.Broken(errStoreDown)
		},
		Write: func(t *testing.T, ref secrets.Ref, plaintext []byte) {
			t.Helper()
			// A fresh token per write, which is the rotation signal the case asks
			// for; a store's own token is its business, and Versions only asks that
			// it move when the bytes do.
			writes[ref]++
			secretsMap[ref] = secrets.Secret{Bytes: plaintext, Version: "v" + strconv.Itoa(writes[ref])}
		},
		WriteKey: func(t *testing.T, ref secrets.Ref) {
			t.Helper()
			keys[ref] = []byte(secretstest.KeyMaterial)
		},
		Exact: []byte{0x00, 0xff, 0xfe, 'a', 0x00, 0x7f, 0xc3, 0x28, '\n'},
	}
	secretstest.Conformance(t, adapter)
	secretstest.Tenants(t, adapter)
	secretstest.Versions(t, adapter)
	secretstest.Keys(t, adapter)
	secretstest.TenantKeys(t, adapter)
}

// TestABrokenStoreIsNotAMiss is the caller's side of the same rule the suite's first
// case makes: a module that fails open when a secret is missing must fail closed when
// the store stopped answering, and it can only do that from the difference between an
// error and ErrNotFound.
func TestABrokenStoreIsNotAMiss(t *testing.T) {
	broken := secrets.Broken(errStoreDown)
	_, err := broken.Get(t.Context(), secrets.Shared("mail.password"))
	if err == nil {
		t.Fatal("a broken store answered nothing")
	}
	if errors.Is(err, secrets.ErrNotFound) {
		t.Errorf("a broken store answered ErrNotFound: %v", err)
	}
}

// TestTheAddressGrammarIsTheOneRefusal pins the subset every store can spell, and so
// the one place traversal is refused: an adapter may join a name into a path, and it
// may do so because nothing that reaches it can hold a slash or a dot-dot.
func TestTheAddressGrammarIsTheOneRefusal(t *testing.T) {
	tenant := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	good := []string{"mail.password", "smtp-host", "factor_key", "a", "a1", "9lives", "a.b.c-d_e"}
	for _, name := range good {
		if got := secrets.Shared(name); got.Name() != name {
			t.Errorf("Shared(%q) was refused", name)
		}
		if got := secrets.Of(tenant, name); got.Name() != name {
			t.Errorf("Of(tenant, %q) was refused", name)
		}
	}
	bad := []string{"", "MiXeD", "../x", "a/b", `a\b`, ".hidden", "trailing.", "-leading",
		string(make([]byte, 64))}
	for _, name := range bad {
		if got := secrets.Shared(name); got.Name() != "" {
			t.Errorf("Shared(%q) was accepted", name)
		}
		if got := secrets.Of(tenant, name); got.Name() != "" {
			t.Errorf("Of(tenant, %q) was accepted", name)
		}
	}
	if got := secrets.Of(uuid.Nil, "mail.password"); got.Name() != "" {
		t.Error("Of(uuid.Nil, …) was accepted; Shared is the door for the installation's own")
	}
}

// TestAnAddressNamesNoValue is the redaction rule in its cheapest form: the string
// that reaches a log line, a span or an error sentence is an address, and the only
// datum in it that a reader could use to enumerate one customer is eight hex digits.
func TestAnAddressNamesNoValue(t *testing.T) {
	tenant := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	shared := secrets.Shared("mail.password").String()
	if shared != "shared/mail.password" {
		t.Errorf("Shared spelled %q", shared)
	}
	owned := secrets.Of(tenant, "smtp.password").String()
	if owned != "tenant/11111111/smtp.password" {
		t.Errorf("Of spelled %q; want the name and eight digits of the tenant", owned)
	}
	if owned == tenant.String()+"/smtp.password" {
		t.Error("an address carries a whole tenant id")
	}
	if zero := (secrets.Ref{}).String(); zero != "(no name)" {
		t.Errorf("the zero Ref spelled %q", zero)
	}
}

// TestAnAddressIsBoundIntoItsSeal is the case the AAD exists for, run at the port's
// own level so the rule is pinned even where an adapter's store cannot hold two keys:
// a seal made for one name refuses to open under another.
func TestAnAddressIsBoundIntoItsSeal(t *testing.T) {
	material := []byte(secretstest.KeyMaterial)
	sealed, err := secrets.SealWith(secrets.Shared("a"), material, []byte("one credential"))
	if err != nil {
		t.Fatalf("SealWith: %v", err)
	}
	if _, err := secrets.OpenWith(secrets.Shared("b"), material, sealed); !errors.Is(err, secrets.ErrSealed) {
		t.Errorf("opening under another name = %v; want ErrSealed", err)
	}
	if _, err := secrets.OpenWith(secrets.Shared("a"), material, sealed[:len(sealed)-3]); !errors.Is(err, secrets.ErrSealed) {
		t.Errorf("opening a truncated seal = %v; want ErrSealed", err)
	}
	for _, material := range [][]byte{nil, []byte("not base64 at all!!"), []byte("aXNob3J0")[:6]} {
		if _, err := secrets.SealWith(secrets.Shared("a"), material, []byte("x")); !errors.Is(err, secrets.ErrSealed) {
			t.Errorf("sealing under %q = %v; want ErrSealed and no panic", material, err)
		}
	}
}
