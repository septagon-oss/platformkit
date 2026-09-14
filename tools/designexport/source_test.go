package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui"
	"github.com/septagon-oss/platformkit/ui/components/examples"
	"github.com/septagon-oss/platformkit/ui/source"
)

func TestSourcePreviewVerifiesRealGalleryWithoutWriting(t *testing.T) {
	const file = "ui/components/examples/gallery.go"
	before := map[string][]byte{}
	for _, name := range []string{file, "go.mod", "go.sum"} {
		data, err := os.ReadFile(filepath.Join("../..", name))
		if err != nil {
			t.Fatal(err)
		}
		before[name] = data
	}
	t.Cleanup(func() {
		for name, want := range before {
			got, err := os.ReadFile(filepath.Join("../..", name))
			if err != nil || !bytes.Equal(got, want) {
				t.Errorf("preview changed %s: %v", name, err)
			}
		}
	})
	marker := []byte(`ExampleWithSlots(info("pk-ui.component.button/primary"`)
	if bytes.Count(before[file], marker) != 1 {
		t.Fatal("primary button capture must have one explicit source occurrence")
	}
	line := bytes.Count(before[file][:bytes.Index(before[file], marker)], []byte("\n")) + 1
	digest := func(data []byte) string { return fmt.Sprintf("%x", sha256.Sum256(data)) }
	captures := examples.Gallery()
	base, err := ui.Export(design.Default(), captures)
	if err != nil {
		t.Fatal(err)
	}
	proposal := ui.PropsProposal{BaseSHA256: base.SHA256, Path: []string{"pk-ui.component.button/primary"}, Props: json.RawMessage(`{"label":"CLI preview & keep"}`)}
	_, expected, err := ui.ProjectProps(design.Default(), captures, proposal)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(proposal)
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"--source", file, "--dir", "../..", "--producer", "./tools/designexport", "--line", strconv.Itoa(line), "--sha256", digest(before[file])}
	var output bytes.Buffer
	if err := run(args, bytes.NewReader(body), &output); err != nil {
		t.Fatal(err)
	}
	var response struct {
		Applied bool          `json:"applied"`
		Change  source.Review `json:"change"`
	}
	if err := json.Unmarshal(output.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	review := response.Change
	if response.Applied || review.BeforeSHA256 != digest(before[file]) || review.AfterSHA256 != digest([]byte(review.Source)) || review.AfterSHA256 == review.BeforeSHA256 {
		t.Fatal("preview did not return an unapplied, changed candidate with correct source revisions")
	}
	if review.BaseSHA256 != base.SHA256 || review.ResultSHA256 != expected.SHA256 || !strings.Contains(review.Source, `Label: "CLI preview & keep"`) {
		t.Fatal("preview differs from the independent full Gallery proposal")
	}
}

func TestSourceCommandRefusesMalformedRequests(t *testing.T) {
	valid := []string{"--source", "ui/components/examples/gallery.go", "--dir", "../..", "--line", "1", "--sha256", strings.Repeat("0", 64)}
	for _, tc := range []struct {
		name, body, want string
		args             []string
	}{
		{"missing-file", `{}`, "needs an argument", []string{"--source"}},
		{"missing-line", `{}`, "requires", []string{"--source", "file.go", "--sha256", strings.Repeat("0", 64)}},
		{"missing-hash", `{}`, "requires", []string{"--source", "file.go", "--line", "1"}},
		{"invalid-line", `{}`, "invalid value", []string{"--source", "file.go", "--line", "invalid"}},
		{"unknown-flag", `{}`, "not defined", []string{"--source", "file.go", "--unknown"}},
		{"invalid-json", `{`, "unexpected end", valid},
		{"nonstring-revision", `{"baseSHA256":42,"path":["x"],"props":{"label":"x"}}`, "cannot unmarshal number", valid},
		{"unknown-proposal-field", `{"unexpected":true}`, "unknown or repeated field", valid},
		{"stale-source", `{"baseSHA256":"old","path":["x"],"props":{"label":"x"}}`, "revision is stale", valid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			err := run(tc.args, strings.NewReader(tc.body), &output)
			if err == nil || !strings.Contains(err.Error(), tc.want) || output.Len() != 0 {
				t.Fatalf("expected %q refusal without output: %v", tc.want, err)
			}
		})
	}
}
