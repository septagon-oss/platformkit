package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// TestTheTokenExportChangesNothingButItsOwnSection states the reach of the
// --tokens opt-in: it adds the sourceTokens section and leaves every other
// section exactly as the plain export renders it. A device reads themes, icons
// and css from the plain export and token values from the opt-in one, so a token
// projection that re-rendered a shared section would put a palette in the shell
// that no served page ever shows. The two exports share no fixture: only a
// reader that renders both can notice them disagree.
func TestTheTokenExportChangesNothingButItsOwnSection(t *testing.T) {
	plain := renderExport(t)
	if _, ok := plain["sourceTokens"]; ok {
		t.Error("the plain export carries a sourceTokens section; only --tokens adds it")
	}

	token := renderExport(t, "--tokens", "both")
	if _, ok := token["sourceTokens"]; !ok {
		t.Fatal("--tokens both wrote no sourceTokens section")
	}

	for _, section := range []string{"fontPolicy", "notices", "themes", "icons", "css"} {
		if !bytes.Equal(plain[section], token[section]) {
			t.Errorf("--tokens changed the %q section as well; the opt-in is only allowed to add sourceTokens", section)
		}
	}
}

// renderExport runs the command with these arguments and returns its JSON object
// by section. A rejected flag, an empty document or a body that is not a JSON
// object fails the caller.
func renderExport(t *testing.T, args ...string) map[string]json.RawMessage {
	t.Helper()
	var out bytes.Buffer
	if err := run(args, strings.NewReader(""), &out); err != nil {
		t.Fatalf("run %v: %v", args, err)
	}
	if out.Len() == 0 {
		t.Fatalf("run %v wrote no bytes", args)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatalf("run %v wrote a document that is not a JSON object: %v", args, err)
	}
	return doc
}
