package export_test

// The pin for the one thing this task exists to fix, and the one done-criterion
// that names no test: LICENSE is the canonical Apache-2.0 text, byte for byte.
//
// It had been edited twice — the project's copyright block inserted at line 5,
// inside the licence body where the preamble sits, and "APPENDIX: How to apply
// the Apache License to your work" deleted — and either edit alone defeats
// GitHub's licence detector, which is why the repository page and the API
// answered NOASSERTION rather than Apache-2.0. A company's legal review stops
// at an unlicensed project.
//
// `gh api repos/septagon-oss/platformkit --jq .license.spdx_id` reads the
// pushed repository and so cannot be a test here. This is what can: the digest
// of the text at apache.org/licenses/LICENSE-2.0.txt, which every Apache-2.0
// distribution on this machine carries (google.golang.org/grpc's LICENSE is one
// of them, md5 3b83ef96387f14655fc854ddc3c6bd57). The copyright line belongs in
// NOTICE, which carries it, and in file headers — never inside the licence body.
//
// ui/export is this file's home because ui/export/design-notices.txt embeds
// LICENSE and NOTICE verbatim so attribution travels with redistributed vector
// bytes, and TestDesignExportUsesCurrentRenderingAndAssets already holds the
// export to what these two files say. This case holds one of them to what the
// detector can read.

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
	"testing"
)

const canonicalApache2 = "cfc7749b96f63bd31c3c42b5c471bf756814053e847c10f3eb003417bc523d30"

func TestTheLicenceIsTheCanonicalApache2TextADetectorCanRead(t *testing.T) {
	text, err := os.ReadFile("../../LICENSE")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(text)
	if got := hex.EncodeToString(sum[:]); got != canonicalApache2 {
		// The two edits that produced NOASSERTION, named so a failure says which
		// one came back rather than only that a digest moved.
		body, _, _ := strings.Cut(string(text), "APPENDIX:")
		t.Fatalf("LICENSE is not the canonical Apache-2.0 text (sha256 %s, want %s): "+
			"appendix present: %v; a copyright line inside the licence body: %v; %d lines, want 202. "+
			"Either edit alone makes GitHub report the licence as NOASSERTION",
			got, canonicalApache2,
			strings.Contains(string(text), "APPENDIX: How to apply the Apache License to your work."),
			strings.Contains(body, "Copyright"),
			strings.Count(string(text), "\n"))
	}
}
