// Package filetest is the conformance suite for contracts.Service, a Storage
// in memory that the suite runs against, and a fake Service that passes it.
//
// It exists because an interface is justified by a passing fake and not by a
// second production implementation (AGENTS.md rule 8). RunService is the
// specification written as executable cases; the real service and the fake both
// run it.
package filetest

import (
	"bytes"
	"errors"
	goimage "image"
	"image/color"
	pngimage "image/png"
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

// open is the Fixture's transaction as an opener, which is what Upload takes:
// the bytes are stored before anything is opened, so the suite has to be able
// to hand over the opening rather than the opened.
func (f Fixture) open(context.Context) (db.Tx[db.Tenant], error) { return f.Tx, nil }

// scope is the tenant this fixture's context carries. The two harnesses both
// put a tenant on it — the Postgres one because db.Run does, the fake's because
// the fake takes its scope from the context the way the real Upload does — and
// the suite asks for the same scope the command under test would derive.
func (f Fixture) scope(t *testing.T) contracts.Scope {
	t.Helper()
	s, err := contracts.ScopeOf(f.Ctx)
	if err != nil {
		t.Fatalf("the fixture's context names no tenant: %v", err)
	}
	return s
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

// image is an upload the caller called an image: the same envelope as upload,
// with the declaration that decides what a refusal is (contracts.Upload.Image).
// The body is bytes rather than a string because it is a PNG.
func asImage(t *testing.T, name, contentType string, body []byte) contracts.Upload {
	t.Helper()
	return contracts.Upload{
		Name: name, ContentType: contentType, Visibility: contracts.VisibilityPrivate,
		Declared: int64(len(body)), Body: bytes.NewReader(body), Image: true,
	}
}

// pngFrame is a real PNG of w x h with one transparent pixel in it, which is
// what decides its container once the pass re-encodes it.
// pngFrame is an opaque w x h PNG: no alpha, which is what makes the pass
// choose JPEG and the case able to see that it chose.
func pngFrame(t *testing.T, w, h int) []byte {
	t.Helper()
	frame := goimage.NewRGBA(goimage.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			frame.Set(x, y, color.RGBA{R: uint8(40 + x*30), G: uint8(90 + y*20), B: 200, A: 255})
		}
	}
	out := &bytes.Buffer{}
	if err := pngimage.Encode(out, frame); err != nil {
		t.Fatalf("encode the fixture: %v", err)
	}
	return out.Bytes()
}

// pngOf encodes an RGBA frame with one see-through corner.
func pngOf(t *testing.T, w, h int) []byte {
	t.Helper()
	frame := goimage.NewRGBA(goimage.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			frame.Set(x, y, color.RGBA{R: uint8(40 + x*20), G: uint8(90 + y*20), B: 200, A: 255})
		}
	}
	frame.Set(0, 0, color.RGBA{B: 200, A: 128})
	out := &bytes.Buffer{}
	if err := pngimage.Encode(out, frame); err != nil {
		t.Fatalf("encode the fixture: %v", err)
	}
	return out.Bytes()
}

// stored uploads one private text file and returns its row.
func stored(t *testing.T, f Fixture, body string) *contracts.File {
	t.Helper()
	out, err := f.Service.Upload(f.Ctx, f.open, upload("notes.txt", "text/plain", contracts.VisibilityPrivate, body))
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
			different := stored(t, f, "different bytes\n")
			if different.SHA256 != "78442dda3e5642ce405f6dc6a36797ab67d4c6bef4f46eb5e19d2c66d7f1595c" || different.SHA256 == first.SHA256 {
				t.Errorf("different content has digest %q, want its own SHA-256", different.SHA256)
			}
		},

		"an upload past the limit keeps nothing": func(t *testing.T, f Fixture) {
			before := len(f.Keys())
			_, err := f.Service.Upload(f.Ctx, f.open, upload("big.bin", "application/octet-stream",
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
			row, err := f.Service.Upload(f.Ctx, f.open, upload("logo.png", "image/png", contracts.VisibilityPublic, png))
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
		},

		// The image pass, in both implementations and from one function
		// (contracts.ProcessImage): the row carries the frame the server stored,
		// and the container follows the pixels rather than the file name, so an
		// opaque frame that arrived as a PNG leaves as a JPEG. That a stored
		// image carries no metadata block is proved over the pass itself, in
		// contracts/image_test.go, where a fixture with a real EXIF and GPS
		// section can be read back; a shared case that re-encodes a PNG writes
		// the same bytes it was given, which is the encoder being deterministic
		// and not a bug the suite should pretend to see.
		"an image is measured and stored as the pixels it re-encodes to": func(t *testing.T, f Fixture) {
			sent := pngFrame(t, 2, 3)
			var row *contracts.File
			f.one(t, "uploading an image", contracts.EventUploaded, func() {
				out, err := f.Service.Upload(f.Ctx, f.open, asImage(t, "image.png", "image/png", sent))
				if err != nil {
					t.Fatalf("Upload: %v", err)
				}
				row = out
			})
			if row.Width != 2 || row.Height != 3 {
				t.Errorf("the row carries %d x %d, want 2 x 3: nobody measured the frame", row.Width, row.Height)
			}
			if row.SHA256 == "" {
				t.Error("the digest is empty")
			}

			// What nobody called an image stays the attachment it arrived as,
			// even when its header claims a frame: refusing it here would change
			// what a document door accepts, and 0069 §4 does not ask for that.
			if row.ContentType != "image/jpeg" {
				t.Errorf("an opaque frame was stored as %s, want image/jpeg: the container follows the pixels", row.ContentType)
			}
			if _, text := read(t, f, row.ID, false); text == string(sent) {
				t.Error("the stored bytes are the ones that arrived, so nothing was re-encoded")
			} else if bytes.Contains([]byte(text), []byte("Exif")) {
				t.Error("the stored bytes carry an Exif block, which is what the pass removes")
			}

			if doc, err := f.Service.Upload(f.Ctx, f.open, upload("frame.png", "image/png", contracts.VisibilityPrivate, string(pngFrame(t, 2, 2)))); err != nil {
				t.Fatalf("the same bytes as a document: %v", err)
			} else if doc.Width != 2 {
				t.Errorf("an unclaimed image carries %d px, want the pass to have run over it too", doc.Width)
			}
		},

		// An SVG with a script in it, offered as an image, never reaches storage.
		"what no decoder reads is refused at the image door and keeps nothing": func(t *testing.T, f Fixture) {
			before := len(f.Keys())
			_, err := f.Service.Upload(f.Ctx, f.open, asImage(t, "diagram.svg", "image/svg+xml",
				[]byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)))
			if !errors.Is(err, contracts.ErrNotImage) {
				t.Errorf("a vector offered as an image = %v, want ErrNotImage", err)
			}
			if err == nil || !strings.Contains(err.Error(), "not an image") {
				t.Errorf("the refusal does not say the reason: %v", err)
			}
			if len(f.Keys()) != before {
				t.Errorf("the refusal left %d objects behind, want none", len(f.Keys())-before)
			}
			if len(f.Published()) != 0 {
				t.Errorf("the refusal published %v, want nothing", f.Published())
			}
		},

		// A use is what a body references, rewritten rather than appended to, and
		// the answer to "what reads this file" is exactly the set of rows the last
		// accepted body wrote. Both implementations run the same two decisions
		// (contracts.CollapseRefs and contracts.DiffUses) over their own storage.
		"a record's uses are its body's references, rewritten in place": func(t *testing.T, f Fixture) {
			first, second := stored(t, f, "one"), stored(t, f, "two")
			record := uuid.New()
			use := contracts.Use{Module: "content", Entity: "page", Record: record, Field: "body", Locale: "en"}
			// A repeat collapses: one field showing the same image twice is one
			// use of it, because the file is kept alive once. And nothing is
			// published: the record's own write is the auditable fact, and the
			// file module has no business emitting an event for somebody else's
			// row, so the suite asks that no event arrived at all.
			before := len(f.Published())
			unused, err := f.Service.SetUses(f.Ctx, f.Tx, use, []uuid.UUID{first.ID, second.ID, first.ID})
			if err != nil {
				t.Fatalf("SetUses: %v", err)
			}
			if got := f.Published()[before:]; len(got) != 0 {
				t.Errorf("recording a use published %v, want nothing", got)
			}
			if len(unused) != 0 {
				t.Errorf("the first write ended %d uses, want none", len(unused))
			}
			rows, err := f.Service.Uses(f.Ctx, f.Tx, first.ID)
			if err != nil {
				t.Fatalf("Uses: %v", err)
			}
			if len(rows) != 1 || rows[0].Use != use {
				t.Errorf("the file is used by %+v, want the one field that named it", rows)
			}
			// The edit that dropped the first image ends its use and names it,
			// and the row does not linger: a use that outlived the body that
			// stopped naming the file is the answer "nothing reads this" being
			// wrong about a file the sweep is about to release.
			out, err := f.Service.SetUses(f.Ctx, f.Tx, use, []uuid.UUID{second.ID})
			if err != nil {
				t.Fatalf("SetUses again: %v", err)
			}
			if len(out) != 1 || out[0] != first.ID {
				t.Errorf("the rewrite ended %v, want the file the body dropped", out)
			}
			if rows, err := f.Service.Uses(f.Ctx, f.Tx, first.ID); err != nil || len(rows) != 0 {
				t.Errorf("the dropped file is still read by %+v (%v), want nobody", rows, err)
			}
			// A body with no images at all leaves nothing, and the second image
			// is still read by the same field.
			if _, err := f.Service.SetUses(f.Ctx, f.Tx, use, nil); err != nil {
				t.Fatalf("a body that shows nothing: %v", err)
			}
			if rows, err := f.Service.Uses(f.Ctx, f.Tx, second.ID); err != nil || len(rows) != 0 {
				t.Errorf("after the body dropped both images it reads %+v (%v), want nobody", rows, err)
			}
		},

		"the uses of a file that is not there are refused and nothing is written": func(t *testing.T, f Fixture) {
			kept := stored(t, f, "kept")
			use := contracts.Use{Module: "content", Entity: "page", Record: uuid.New(), Field: "body", Locale: "en"}
			_, err := f.Service.SetUses(f.Ctx, f.Tx, use, []uuid.UUID{kept.ID, uuid.New()})
			if !errors.Is(err, crud.ErrNotFound) {
				t.Fatalf("a body naming a gone file = %v, want ErrNotFound", err)
			}
			if !strings.Contains(err.Error(), "file") {
				t.Errorf("the refusal does not name the file it refused: %v", err)
			}
			// The id that was there is not half-written: a use of the files that
			// happen to still exist would say a record shows what it does not.
			rows, uerr := f.Service.Uses(f.Ctx, f.Tx, kept.ID)
			if uerr != nil || len(rows) != 0 {
				t.Errorf("the refused write left %+v (%v), want nothing", rows, uerr)
			}
		},

		// Ending the use of a file that was removed first is not the refusal
		// above, and a record has to survive the difference: Delete consults the
		// ledger no more than it does today, so the row that names a gone file is
		// ordinary rather than corrupt, and the delete of the record that shows it
		// must go through. Refusing here would strand that record and leave the
		// ledger saying a file nobody has is still being shown.
		"a use ended of a file that is already gone ends": func(t *testing.T, f Fixture) {
			gone, kept := stored(t, f, "shown then removed"), stored(t, f, "still shown")
			use := contracts.Use{Module: "content", Entity: "page", Record: uuid.New(), Field: "body", Locale: "en"}
			if _, err := f.Service.SetUses(f.Ctx, f.Tx, use, []uuid.UUID{gone.ID, kept.ID}); err != nil {
				t.Fatalf("SetUses: %v", err)
			}
			if _, err := f.Service.Delete(f.Ctx, f.Tx, gone.ID); err != nil {
				t.Fatalf("Delete: %v", err)
			}
			if _, err := f.Service.SetUses(f.Ctx, f.Tx, use, []uuid.UUID{kept.ID}); err != nil {
				t.Fatalf("the edit that dropped a removed image = %v, want it accepted", err)
			}
			if rows, err := f.Service.Uses(f.Ctx, f.Tx, kept.ID); err != nil || len(rows) != 1 || rows[0].Use != use {
				t.Errorf("the file still shown reads by %+v (%v), want the one field showing it", rows, err)
			}
			// The same rewrite again is the test that the dangling row went with
			// the first one: were it still there, this call would end it a second
			// time and name it as newly unused.
			out, err := f.Service.SetUses(f.Ctx, f.Tx, use, []uuid.UUID{kept.ID})
			if err != nil || len(out) != 0 {
				t.Errorf("rewriting the same body again ended %v (%v), want nothing", out, err)
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
			if _, err := f.Storage.Get(f.Ctx, f.scope(t), contracts.Key(row.StorageKey)); err != nil {
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
				if _, err := f.Service.Upload(f.Ctx, f.open, up); !errors.Is(err, crud.ErrInvalid) {
					t.Errorf("Upload(%q/%q) = %v, want ErrInvalid", up.Name, up.Visibility, err)
				}
			}
		},
	}
}
