package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClusterVersionMarkerOutputOmitsUntrustedContents(t *testing.T) {
	data := t.TempDir()
	cluster := filepath.Join(data, "postgres")
	if err := os.Mkdir(cluster, 0o700); err != nil {
		t.Fatal(err)
	}
	const sentinel = "private-token-from-corrupt-marker"
	if err := os.WriteFile(filepath.Join(cluster, "PG_VERSION"), []byte(sentinel), 0o600); err != nil {
		t.Fatal(err)
	}
	got := embeddedClusterObservation(data)
	if strings.Contains(got.Observed, sentinel) || strings.Contains(got.Next, sentinel) {
		t.Fatalf("cluster marker contents escaped into diagnosis: %+v", got)
	}
}
