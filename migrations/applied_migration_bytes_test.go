package migrations_test

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"testing"
)

func TestAppliedModuleMigrationsKeepTheirOriginalBytes(t *testing.T) {
	for _, original := range []struct {
		owner, name, checksum string
	}{
		{"auth", "000035_passkeys.up.sql", "e9d807bdff39e40f7bdf0107fbe6d8e9bfe33b4a3f136263e83891009307c42e"},
		{"task", "000050_task_revision.up.sql", "90f1153fabe2a55a2e76f35159fa5a8b2485fb63f28fca145e65d71619a0a971"},
	} {
		t.Run(original.owner, func(t *testing.T) {
			for _, source := range everything {
				if source.Owner != original.owner {
					continue
				}
				body, err := fs.ReadFile(source.Files, original.name)
				if err != nil {
					t.Fatalf("read applied migration %s/%s: %v", original.owner, original.name, err)
				}
				if got := fmt.Sprintf("%x", sha256.Sum256(body)); got != original.checksum {
					t.Fatalf("applied migration %s/%s changed: checksum %s, want %s", original.owner, original.name, got, original.checksum)
				}
				return
			}
			t.Fatalf("composition omits migration owner %s", original.owner)
		})
	}
}
