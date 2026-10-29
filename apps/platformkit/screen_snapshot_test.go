package main

import (
	"bytes"
	"os"
	"testing"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/screen"
)

func TestScreenSnapshotMatchesTwoFreshCompositions(t *testing.T) {
	cfg, err := config.Load("testdata/composition.yaml")
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("../../ui/screens/testdata/screens.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := screen.ParseSnapshot(want); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		inventory, err := referenceScreens(cfg)
		if err != nil {
			t.Fatal(err)
		}
		got, err := inventory.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatal("screen artifact differs from actual composition; review screens -update")
		}
	}
}
