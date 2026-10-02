package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/config"
)

// TestFilesRetentionIsATableOfDurations is the deployment's half of a retention
// policy: which class lives how long, keyed by the `kind` an upload carried.
// modules/file matches one token against the other and interprets neither, so
// the only thing the loader owes is that a duration means what a reader of the
// file thought it meant.
//
// The bare number in the last case is the accident this shape exists to make
// impossible. A duration is a number of nanoseconds to a decoder that is not
// told otherwise, so `invoice: 365` would be a policy of 365ns — every invoice
// older than the previous sweep gone on the next tick. It is refused here, by
// name, instead.
func TestFilesRetentionIsATableOfDurations(t *testing.T) {
	got, err := config.Load(edit(t, "retention: {}",
		"retention:\n    invoice: \"8760h\"\n    chat_photo: \"45m\""))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got.Files.Retention) != 2 {
		t.Fatalf("files.retention = %v, want the two classes the file names", got.Files.Retention)
	}
	if got.Files.Retention["invoice"] != 8760*time.Hour || got.Files.Retention["chat_photo"] != 45*time.Minute {
		t.Errorf("files.retention = %v, want 8760h and 45m", got.Files.Retention)
	}

	// The example ships no policy, which is the safe default and not a missing
	// one: with nothing in the table no class has a cutoff, and the sweep the
	// composition would schedule deletes nothing and says so in the log.
	if plain, err := config.Load(example); err != nil || len(plain.Files.Retention) != 0 {
		t.Errorf("%s carries %v, %v; want an empty table", example, plain.Files.Retention, err)
	}

	for _, bad := range []struct{ body, want string }{
		{`invoice: "0s"`, `files.retention.invoice is 0s`},
		{`invoice: "-24h"`, `files.retention.invoice is -24h0m0s`},
		{`invoice: 365`, `cannot unmarshal !!int`},
	} {
		path := edit(t, "retention: {}", "retention:\n    "+bad.body)
		if _, err := config.Load(path); err == nil || !strings.Contains(err.Error(), bad.want) {
			t.Errorf("files.retention with %s = %v, want it refused for %q", bad.body, err, bad.want)
		}
	}
}

// TestAnEmptyRetentionKeyIsRefused is the one entry that would be a policy over
// everything: an upload that named no class has kind ”, and a table keyed on
// nothing is the widest deletion in this application wearing the shape of a
// class. The case above refuses it too; this one says why out loud.
func TestAnEmptyRetentionKeyIsRefused(t *testing.T) {
	_, err := config.Load(edit(t, "retention: {}", "retention:\n    \"\": \"24h\""))
	if err == nil || !strings.Contains(err.Error(), "is not a class") {
		t.Fatalf("an entry keyed on nothing = %v, want the load to refuse it as the absence of a class", err)
	}
}
