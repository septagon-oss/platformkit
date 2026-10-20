package ui_test

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui"
)

// The SSE extension is the second third-party byte this kernel ships to a browser,
// and the one whose behaviour nobody here re-reads after the day it was fetched. Its
// bytes are therefore pinned twice over: the digest is recorded in NOTICE beside the
// licence that covers them, and this case re-reads the digest from what the build
// actually embeds, so an edit to the file fails the record here rather than in a
// person's browser.
const (
	vendoredSSE    = "js/htmx-ext-sse-2.2.4.min.js"
	vendoredSSESha = "98a46496de0c3605fbffdce9167ba427bdd9553184f83f149c261891a92c0136"
	vendoredSSELen = 2853
)

func TestTheVendoredSSEExtensionIsThePinnedBytes(t *testing.T) {
	t.Parallel()
	f, err := ui.Assets(ui.Compose(design.Default())).Open(vendoredSSE)
	if err != nil {
		t.Fatalf("the pinned extension is not served: %v", err)
	}
	defer f.Close()
	body, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) != vendoredSSELen {
		t.Errorf("%s is %d bytes, not the %d the record names", vendoredSSE, len(body), vendoredSSELen)
	}
	sum := sha256.Sum256(body)
	if got := hex.EncodeToString(sum[:]); got != vendoredSSESha {
		t.Errorf("%s digests to %s, not the pinned %s: the file changed, and NOTICE must be restated in the same commit",
			vendoredSSE, got, vendoredSSESha)
	}
	if !strings.Contains(string(body), `htmx.defineExtension("sse"`) {
		t.Errorf("%s defines no extension named sse, which is the value hx-ext carries", vendoredSSE)
	}
}

// A digest is only a pin if a reader can find it, so the record and the bytes are
// read against each other rather than trusted separately.
func TestTheLicenceRecordCarriesTheDigestItPins(t *testing.T) {
	t.Parallel()
	notice, err := os.ReadFile("../NOTICE")
	if err != nil {
		t.Fatal(err)
	}
	text := string(notice)
	for _, want := range []string{
		vendoredSSESha,
		"htmx-ext-sse 2.2.4",
		"Zero-Clause BSD",
		"Alexander Petros",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("NOTICE does not record %q for the vendored extension", want)
		}
	}
}

// The extension is not one of the controllers: it belongs to the pages that name a
// topic, and the list of what every page downloads must stay the answer to that
// question alone.
func TestTheSSEExtensionIsNotLoadedByEveryPage(t *testing.T) {
	t.Parallel()
	for _, name := range ui.Controllers {
		if strings.Contains(name, "sse") {
			t.Errorf("the live extension sits in the list every page loads: %s", name)
		}
	}
	if !strings.HasSuffix(ui.SSEExtension, ".min.js") {
		t.Errorf("the vendored file is named %s: the hand-written JavaScript budget excludes a minified engine by that suffix, and a pin whose name drops it would count upstream bytes as this repository's own", ui.SSEExtension)
	}
}
