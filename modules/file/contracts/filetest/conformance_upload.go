package filetest

import (
	"errors"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
)

func countAndHashUpload(t *testing.T, f Fixture) {
	var row *contracts.File
	f.one(t, "uploading", contracts.EventUploaded, func() { row = stored(t, f, hello) })
	switch {
	case row.Size != int64(len(hello)):
		t.Errorf("the size is %d, want %d: it is what arrived and not what anybody said", row.Size, len(hello))
	case row.SHA256 != "de87f09feb74b4bc17d5209e2d50276aae7fc6f8f0bb7b84a111e6f81a3a79c1":
		t.Errorf("the digest is %q, want the SHA-256 of hello, files followed by a newline", row.SHA256)
	case row.StorageKey == "" || row.StorageKey == row.ID.String():
		t.Errorf("the storage key is %q; it is a UUID of its own", row.StorageKey)
	case row.Visibility != contracts.VisibilityPrivate:
		t.Errorf("visibility is %q, want private by default", row.Visibility)
	}
	if _, back := read(t, f, row.ID, false); back != hello {
		t.Errorf("the bytes read back as %q, want %q", back, hello)
	}
}

func separateRepeatedUploads(t *testing.T, f Fixture) {
	first, second := stored(t, f, hello), stored(t, f, hello)
	switch {
	case first.ID == second.ID:
		t.Error("one row for two uploads")
	case first.StorageKey == second.StorageKey:
		t.Error("two rows share a storage key; a key is minted per upload")
	case first.SHA256 != second.SHA256:
		t.Error("the same bytes hashed differently")
	}
	different := stored(t, f, "different bytes\n")
	if different.SHA256 != "78442dda3e5642ce405f6dc6a36797ab67d4c6bef4f46eb5e19d2c66d7f1595c" || different.SHA256 == first.SHA256 {
		t.Errorf("different content has digest %q, want its own SHA-256", different.SHA256)
	}
}

func refuseOversizeUpload(t *testing.T, f Fixture) {
	before := len(f.Keys())
	_, err := f.Service.Upload(f.Ctx, f.open, upload("big.bin", "application/octet-stream",
		contracts.VisibilityPrivate, strings.Repeat("x", Limit+1)))
	if !errors.Is(err, contracts.ErrTooLarge) {
		t.Fatalf("an upload of %d bytes = %v, want ErrTooLarge", Limit+1, err)
	}
	if after := f.Keys(); len(after) != before {
		t.Errorf("the storage holds %v after a refusal; a caller that was refused is not charged for storage", after)
	}
}

func acceptExactLimitUpload(t *testing.T, f Fixture) {
	row := stored(t, f, strings.Repeat("x", Limit))
	if row.Size != Limit {
		t.Errorf("the size is %d, want the limit exactly", row.Size)
	}
}

// Inline content must match its declared type so a download cannot disguise active content.
func refuseMIMEMismatch(t *testing.T, f Fixture) {
	_, err := f.Service.Upload(f.Ctx, f.open, upload("logo.png", "image/png", contracts.VisibilityPublic, "<html><script>alert(1)</script>"))
	if !errors.Is(err, crud.ErrInvalid) {
		t.Errorf("a page uploaded as an image = %v, want ErrInvalid", err)
	}
	// The types that are never rendered are not sniffed at all: what a
	// .docx really is, is not this module's business, and a sniffer
	// that had an opinion about every format would refuse half of them.
	if _, err := f.Service.Upload(f.Ctx, f.open, upload("x.bin", "application/octet-stream", contracts.VisibilityPrivate, "<html>")); err != nil {
		t.Errorf("an attachment that is not what it claims = %v, want it stored", err)
	}
}

func validateFile(t *testing.T, f Fixture) {
	for _, up := range []contracts.Upload{
		upload("  ", "text/plain", contracts.VisibilityPrivate, hello),
		upload("notes.txt", "text/plain", "everybody", hello),
	} {
		if _, err := f.Service.Upload(f.Ctx, f.open, up); !errors.Is(err, crud.ErrInvalid) {
			t.Errorf("Upload(%q/%q) = %v, want ErrInvalid", up.Name, up.Visibility, err)
		}
	}
}
