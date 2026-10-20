package file_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/secrets"
	"github.com/septagon-oss/platformkit/kit/secrets/providers/file"
	"github.com/septagon-oss/platformkit/kit/secrets/secretstest"
)

var tenantA = uuid.MustParse("11111111-1111-4111-8111-111111111111")

// TestTheMountedDirectoryConforms runs every case the store can answer: it holds a
// tenant's secrets, it has a version, and it seals under the keys beside the secrets.
func TestTheMountedDirectoryConforms(t *testing.T) {
	dir := t.TempDir()
	adapter := secretstest.Adapter{
		Name:     "file",
		Store:    func(*testing.T) secrets.Store { return mustStore(t, dir) },
		Failing:  func(*testing.T) secrets.Store { return broken(t) },
		Write:    writeSecret(dir),
		WriteKey: writeKey(dir),
		Exact:    []byte{0x00, 0xff, 0xfe, 'a', 0x00, 0x7f, 0xc3, 0x28, '\n'},
	}
	secretstest.Conformance(t, adapter)
	secretstest.Tenants(t, adapter)
	secretstest.Versions(t, adapter)
	secretstest.Keys(t, adapter)
	secretstest.TenantKeys(t, adapter)
}

// TestTheVersionIsAUTCInstant is the store's token spelled the one way: RFC3339Nano
// in UTC, so two processes reading one file agree on the string and a comparison of
// two of them is a comparison of times rather than of localisations.
func TestTheVersionIsAUTCInstant(t *testing.T) {
	dir := t.TempDir()
	writeSecret(dir)(t, secrets.Shared("mail.password"), []byte("one"))
	secret, err := mustStore(t, dir).Get(context.Background(), secrets.Shared("mail.password"))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?Z$`).MatchString(secret.Version) {
		t.Errorf("Version = %q; want a UTC instant", secret.Version)
	}
}

// TestTheBytesAreReturnedExactly is the newline rule: a mounted secret keeps what
// its producer wrote, because a value whose trailing newline is part of it is not
// this adapter's to guess about. The carrier that speaks the protocol trims.
func TestTheBytesAreReturnedExactly(t *testing.T) {
	dir := t.TempDir()
	writeSecret(dir)(t, secrets.Shared("mail.password"), []byte("hunter2\n"))
	secret, err := mustStore(t, dir).Get(context.Background(), secrets.Shared("mail.password"))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(secret.Bytes) != "hunter2\n" {
		t.Errorf("Get = %q; want the mounted bytes untouched", secret.Bytes)
	}
}

// TestNothingOutsideTheDirectoryIsASecret is the traversal case: a name cannot
// escape by being spelled (kit/secrets refuses a slash), and it cannot escape by
// being linked, because a link that lands outside the store is not a secret anybody
// mounted here. A directory and a dangling link are misses for the same reason.
func TestNothingOutsideTheDirectoryIsASecret(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "passwd")
	if err := os.WriteFile(outside, []byte("root:x:0:0"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, prepare := range map[string]func(t *testing.T, dir, path string){
		"a link out of the store": func(t *testing.T, _, path string) {
			if err := os.Symlink(outside, path); err != nil {
				t.Fatal(err)
			}
		},
		"a directory of that name": func(t *testing.T, _, path string) {
			if err := os.Mkdir(path, 0o755); err != nil {
				t.Fatal(err)
			}
		},
		"a dangling link": func(t *testing.T, dir, path string) {
			if err := os.Symlink(filepath.Join(dir, "gone"), path); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			prepare(t, dir, filepath.Join(dir, "escape"))
			_, err := mustStore(t, dir).Get(context.Background(), secrets.Shared("escape"))
			if !errors.Is(err, secrets.ErrNotFound) {
				t.Errorf("Get = %v; want ErrNotFound and none of the bytes", err)
			}
		})
	}
}

// TestAMountedLayoutIsTheOneTwoTenantsCannotCollide is the layout as an operator
// writes it: the installation's name at the top, one subdirectory per customer.
func TestAMountedLayoutIsTheOneTwoTenantsCannotCollide(t *testing.T) {
	dir := t.TempDir()
	writeSecret(dir)(t, secrets.Shared("mail.password"), []byte("installation"))
	writeSecret(dir)(t, secrets.Of(tenantA, "mail.password"), []byte("customer"))
	own, err := mustStore(t, dir).Get(context.Background(), secrets.Shared("mail.password"))
	if err != nil || string(own.Bytes) != "installation" {
		t.Fatalf("the installation's name answered %q, %v", own.Bytes, err)
	}
	if _, err := os.Stat(filepath.Join(dir, tenantA.String(), "mail.password")); err != nil {
		t.Errorf("a tenant's secret is not under its own directory: %v", err)
	}
}

// TestADirectoryThatIsNotThereIsRefusedAtComposition is the boot promise: a store
// named and unreadable refuses the composition rather than the first request.
func TestADirectoryThatIsNotThereIsRefusedAtComposition(t *testing.T) {
	if _, err := file.New(config.Secrets{Adapter: "file", Dir: filepath.Join(t.TempDir(), "absent")}); err == nil {
		t.Error("New accepted a directory that is not there")
	}
	if _, err := file.New(config.Secrets{Adapter: "file", Dir: "relative"}); err == nil {
		t.Error("New accepted a relative secrets.dir")
	}
	if _, err := file.New(config.Secrets{Adapter: "file"}); err == nil {
		t.Error("New accepted an empty secrets.dir")
	}
}

// write puts bytes at the address's own path, making the tenant's subdirectory the
// way a projection would, and bumps the mtime on a rewrite so the version case can
// see a rotation on a filesystem whose clock ticks in whole seconds.
func writeSecret(dir string) secretstest.Write {
	return func(t *testing.T, ref secrets.Ref, plaintext []byte) {
		t.Helper()
		writeBytes(t, dir, ref, plaintext)
	}
}

func writeKey(dir string) secretstest.WriteKey {
	return func(t *testing.T, ref secrets.Ref) {
		t.Helper()
		writeBytes(t, dir, ref, []byte(secretstest.KeyMaterial))
	}
}

func writeBytes(t *testing.T, dir string, ref secrets.Ref, plaintext []byte) {
	t.Helper()
	path := filepath.Join(dir, ref.Name())
	if ref.Tenant() != uuid.Nil {
		path = filepath.Join(dir, ref.Tenant().String(), ref.Name())
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("tenant directory: %v", err)
		}
	}
	if _, err := os.Stat(path); err == nil {
		_ = os.Remove(path)
	}
	if err := os.WriteFile(path, plaintext, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	// A rewrite inside one clock tick would leave the mtime — the version — moved
	// nowhere, so the fixture moves it forward explicitly rather than sleeping.
	stamp := time.Now().Add(time.Second)
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatalf("stamp %s: %v", path, err)
	}
}

func mustStore(t *testing.T, dir string) secrets.Store {
	t.Helper()
	store, err := file.New(config.Secrets{Adapter: "file", Dir: dir})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return store
}

// broken is the same adapter over a directory that stopped existing: the outage an
// adapter must answer as an error that is not ErrNotFound.
func broken(t *testing.T) secrets.Store {
	t.Helper()
	dir := t.TempDir()
	store := mustStore(t, dir)
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("remove the store: %v", err)
	}
	return store
}
