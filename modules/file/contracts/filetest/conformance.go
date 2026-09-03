// Package filetest is the conformance suite for contracts.Service, a Storage
// in memory that the suite runs against, and a fake Service that passes it.
//
// It exists because an interface is justified by a passing fake and not by a
// second production implementation (AGENTS.md rule 8). RunService is the
// specification written as executable cases; the real service and the fake both
// run it.
package filetest

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/file/contracts"

	"context"
)

// Fixture is one case's world: a Service, the transaction its commands take,
// and the storage behind it, so a case can check that the bytes went where the
// row says they did.
type Fixture struct {
	Ctx     context.Context
	Tx      db.Tx[db.Tenant]
	Service contracts.Service
	// Storage is the same store the Service was built on, for the cases about
	// what is left behind: an upload that was refused, and a delete that has
	// not been handled yet.
	Storage contracts.Storage
	// Keys is every key the storage currently holds, which is the one question
	// contracts.Storage does not answer and the only way to see an orphan.
	Keys func() []string
	// Published is the events the implementation has published, in order.
	Published func() []string
}

func (f Fixture) one(t *testing.T, what, want string, step func()) {
	t.Helper()
	before := len(f.Published())
	step()
	got := f.Published()[before:]
	if len(got) != 1 || got[0] != want {
		t.Errorf("%s published %v, want [%s]", what, got, want)
	}
}

// Harness builds one Fixture and calls run with it.
type Harness func(t *testing.T, run func(Fixture))

// Limit is the largest upload the suite's implementations accept. It is small
// so that a case can go past it without allocating anything worth mentioning.
const Limit = 1 << 10

// RunService is the conformance suite. Every implementation of
// contracts.Service passes it, or it is not one.
func RunService(t *testing.T, h Harness) {
	t.Helper()
	for name, run := range cases() {
		t.Run(name, func(t *testing.T) {
			h(t, func(f Fixture) { run(t, f) })
		})
	}
}

// upload is one file arriving, with the body given as a string.
func upload(name, contentType, visibility, body string) contracts.Upload {
	return contracts.Upload{
		Name: name, ContentType: contentType, Visibility: visibility,
		Declared: -1, Body: strings.NewReader(body),
	}
}

// stored uploads one private text file and returns its row.
func stored(t *testing.T, f Fixture, body string) *contracts.File {
	t.Helper()
	out, err := f.Service.Upload(f.Ctx, f.Tx, upload("notes.txt", "text/plain", contracts.VisibilityPrivate, body))
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	return out
}

// read is everything Open gives back, as a string.
func read(t *testing.T, f Fixture, id uuid.UUID, anonymous bool) (*contracts.File, string) {
	t.Helper()
	row, body, err := f.Service.Open(f.Ctx, f.Tx, id, anonymous)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer body.Close()
	out, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("read the bytes: %v", err)
	}
	return row, string(out)
}

// hello is the body most cases upload.
const hello = "hello, files\n"

// png is eight bytes of a real PNG: the signature http.DetectContentType reads.
// A conformance suite that uploaded "not really a png" as image/png used to
// pass, and the upload now refuses it — a file this application would show
// inline has to be what it says it is. See internal.agrees.
const png = "\x89PNG\r\n\x1a\n"

func cases() map[string]func(*testing.T, Fixture) {
	return map[string]func(*testing.T, Fixture){
		"an upload is counted and hashed as it goes past": func(t *testing.T, f Fixture) {
			var row *contracts.File
			f.one(t, "uploading", contracts.EventUploaded, func() { row = stored(t, f, hello) })
			switch {
			case row.Size != int64(len(hello)):
				t.Errorf("the size is %d, want %d: it is what arrived and not what anybody said", row.Size, len(hello))
			case len(row.SHA256) != 64:
				t.Errorf("the digest is %q, want a hex SHA-256", row.SHA256)
			case row.StorageKey == "" || row.StorageKey == row.ID.String():
				t.Errorf("the storage key is %q; it is a UUID of its own", row.StorageKey)
			case row.Visibility != contracts.VisibilityPrivate:
				t.Errorf("visibility is %q, want private by default", row.Visibility)
			}
			if _, back := read(t, f, row.ID, false); back != hello {
				t.Errorf("the bytes read back as %q, want %q", back, hello)
			}
		},

		"the same bytes twice are two files": func(t *testing.T, f Fixture) {
			first, second := stored(t, f, hello), stored(t, f, hello)
			switch {
			case first.ID == second.ID:
				t.Error("one row for two uploads")
			case first.StorageKey == second.StorageKey:
				t.Error("two rows share a storage key; a key is minted per upload")
			case first.SHA256 != second.SHA256:
				t.Error("the same bytes hashed differently")
			}
		},

		"an upload past the limit keeps nothing": func(t *testing.T, f Fixture) {
			before := len(f.Keys())
			_, err := f.Service.Upload(f.Ctx, f.Tx, upload("big.bin", "application/octet-stream",
				contracts.VisibilityPrivate, strings.Repeat("x", Limit+1)))
			if !errors.Is(err, contracts.ErrTooLarge) {
				t.Fatalf("an upload of %d bytes = %v, want ErrTooLarge", Limit+1, err)
			}
			if after := f.Keys(); len(after) != before {
				t.Errorf("the storage holds %v after a refusal; a caller that was refused is not charged for storage", after)
			}
		},

		"an upload of exactly the limit is kept": func(t *testing.T, f Fixture) {
			row := stored(t, f, strings.Repeat("x", Limit))
			if row.Size != Limit {
				t.Errorf("the size is %d, want the limit exactly", row.Size)
			}
		},

		"a private file is not found at the public door": func(t *testing.T, f Fixture) {
			row := stored(t, f, hello)
			// Not forbidden: a caller who is not signed in learns nothing about
			// what this tenant has, including whether it has this.
			if _, _, err := f.Service.Open(f.Ctx, f.Tx, row.ID, true); !errors.Is(err, crud.ErrNotFound) {
				t.Errorf("a private file at the public door = %v, want ErrNotFound", err)
			}
		},

		"a public file is served to anybody": func(t *testing.T, f Fixture) {
			row, err := f.Service.Upload(f.Ctx, f.Tx, upload("logo.png", "image/png", contracts.VisibilityPublic, png))
			if err != nil {
				t.Fatalf("Upload: %v", err)
			}
			got, back := read(t, f, row.ID, true)
			if back != png || got.ContentType != "image/png" {
				t.Errorf("the public file read back as %q/%q", got.ContentType, back)
			}
		},

		// The bytes have to be what the upload said they were, for the types
		// this application will show in a browser. An HTML document uploaded as
		// an image is the second half of the stored-XSS story the download's
		// Content-Disposition is the first half of: served inline as image/png
		// a browser would not run it, but a proxy that rewrites a type, a
		// caller that saves and opens it, and the next media type somebody adds
		// to the inline set are three ways for it to matter.
		"an upload whose bytes disagree with its type is refused": func(t *testing.T, f Fixture) {
			_, err := f.Service.Upload(f.Ctx, f.Tx, upload("logo.png", "image/png", contracts.VisibilityPublic, "<html><script>alert(1)</script>"))
			if !errors.Is(err, crud.ErrInvalid) {
				t.Errorf("a page uploaded as an image = %v, want ErrInvalid", err)
			}
			// The types that are never rendered are not sniffed at all: what a
			// .docx really is, is not this module's business, and a sniffer
			// that had an opinion about every format would refuse half of them.
			if _, err := f.Service.Upload(f.Ctx, f.Tx, upload("x.bin", "application/octet-stream", contracts.VisibilityPrivate, "<html>")); err != nil {
				t.Errorf("an attachment that is not what it claims = %v, want it stored", err)
			}
		},

		"deleting removes the row and says where the bytes are": func(t *testing.T, f Fixture) {
			row := stored(t, f, hello)
			var gone *contracts.File
			f.one(t, "deleting", contracts.EventDeleted, func() {
				var err error
				if gone, err = f.Service.Delete(f.Ctx, f.Tx, row.ID); err != nil {
					t.Fatalf("Delete: %v", err)
				}
			})
			if gone.StorageKey != row.StorageKey {
				t.Errorf("the delete reports key %q, want %q: the event is what carries it once the row is gone", gone.StorageKey, row.StorageKey)
			}
			// The bytes are still there. Removing them is work for after this
			// transaction commits, which is the subscription's, not the
			// command's — a blob delete is not something a rollback can undo.
			if _, err := f.Storage.Get(f.Ctx, row.StorageKey); err != nil {
				t.Errorf("the bytes went with the row: %v", err)
			}
			if _, _, err := f.Service.Open(f.Ctx, f.Tx, row.ID, false); !errors.Is(err, crud.ErrNotFound) {
				t.Errorf("the deleted file is still readable = %v", err)
			}
			if _, err := f.Service.Delete(f.Ctx, f.Tx, row.ID); !errors.Is(err, crud.ErrNotFound) {
				t.Errorf("deleting it twice = %v, want ErrNotFound", err)
			}
		},

		"an unknown id is not found": func(t *testing.T, f Fixture) {
			id := uuid.New()
			if _, _, err := f.Service.Open(f.Ctx, f.Tx, id, false); !errors.Is(err, crud.ErrNotFound) {
				t.Errorf("Open of an unknown file = %v, want ErrNotFound", err)
			}
			if _, err := f.Service.Delete(f.Ctx, f.Tx, id); !errors.Is(err, crud.ErrNotFound) {
				t.Errorf("Delete of an unknown file = %v, want ErrNotFound", err)
			}
		},

		"a file needs a name and a visibility that is one of two": func(t *testing.T, f Fixture) {
			for _, up := range []contracts.Upload{
				upload("  ", "text/plain", contracts.VisibilityPrivate, hello),
				upload("notes.txt", "text/plain", "everybody", hello),
			} {
				if _, err := f.Service.Upload(f.Ctx, f.Tx, up); !errors.Is(err, crud.ErrInvalid) {
					t.Errorf("Upload(%q/%q) = %v, want ErrInvalid", up.Name, up.Visibility, err)
				}
			}
		},
	}
}
