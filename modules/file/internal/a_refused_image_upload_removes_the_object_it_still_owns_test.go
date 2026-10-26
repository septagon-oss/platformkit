package internal_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"io"
	"slices"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/file"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
	"github.com/septagon-oss/platformkit/modules/file/internal"
)

// errNoConnection is the transaction this module was promised and did not get.
var errNoConnection = errors.New("no connection to open a transaction with")

// keptAside is a store that remembers what it was written and what it was asked
// to remove, in the order it was asked. An image upload writes two objects and
// must remove both of them by the time it returns a refusal, so the answer to
// "did the refusal remove the right one" is a list of names, not a count.
type keptAside struct {
	contracts.Storage
	puts    []string
	deletes []string
}

func (k *keptAside) Put(ctx context.Context, scope contracts.Scope, key contracts.Key, r io.Reader, size int64, meta contracts.Meta) error {
	if err := k.Storage.Put(ctx, scope, key, r, size, meta); err != nil {
		return err
	}
	k.puts = append(k.puts, key.String())
	return nil
}

func (k *keptAside) Delete(ctx context.Context, scope contracts.Scope, key contracts.Key) error {
	if err := k.Storage.Delete(ctx, scope, key); err != nil {
		return err
	}
	k.deletes = append(k.deletes, key.String())
	return nil
}

func jpegOf(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h)), nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestAnUploadWhoseTransactionWillNotOpenRemovesTheFrameItStored is the refusal
// that arrives earliest after the bytes are on disk: the image pass has run, the
// frame is stored under a key of its own, the object that arrived is already
// gone, and then no connection opens. Whatever the upload wrote it must also
// have removed — the frame included, because the row that would have named it is
// never written and no reader will ever fetch it.
//
// The two objects are what makes this case say something: a store left with the
// frame holds a byte no row names, and the local adapter sweeps only blobs older
// than an hour while the S3 adapter runs no sweep at all.
func TestAnUploadWhoseTransactionWillNotOpenRemovesTheFrameItStored(t *testing.T) {
	_, conn := dbtest.Schema(t, file.Migrations)
	dir := t.TempDir()
	store := &keptAside{Storage: internal.NewLocal(dir)}
	svc := internal.NewService(store, 16<<20, 0, 0)

	body := jpegOf(t, 40, 20)
	ctx := tenancy.WithTenant(t.Context(), acme)
	_, err := svc.Upload(ctx, func(context.Context) (db.Tx[db.Tenant], error) {
		return db.Tx[db.Tenant]{}, errNoConnection
	}, contracts.Upload{
		Name: "frame.jpg", ContentType: "image/jpeg",
		Body: bytes.NewReader(body), Declared: int64(len(body)),
	})
	if !errors.Is(err, errNoConnection) {
		t.Fatalf("upload with no transaction = %v, want the opener's own error", err)
	}
	// Both objects, in this order: what arrived, then the re-encoded frame. The
	// image pass ran, so the refusal the case asks about is the one it reached.
	if len(store.puts) != 2 {
		t.Fatalf("the upload wrote %v, want the arrived bytes and their frame", store.puts)
	}
	slices.Sort(store.puts)
	slices.Sort(store.deletes)
	if !slices.Equal(store.puts, store.deletes) {
		t.Errorf("a refused image upload removed %v of the %v it wrote, want every object it wrote removed", store.deletes, store.puts)
	}
	if got := keysUnder(t, dir); len(got) != 0 {
		t.Errorf("the refused upload left %v in the store, want no byte of it", got)
	}
	var rows int64
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Table("files").Count(&rows).Error
	}); err != nil {
		t.Fatalf("count the rows: %v", err)
	}
	if rows != 0 {
		t.Errorf("the refused upload wrote %d file row(s), want none", rows)
	}
}

// TestAnImageUploadThatSucceedsLeavesTheFrameAloneInTheStore is the same
// invariant read from the other end: the object that survives a good upload is
// exactly one, and it is the one the row names. The arrived bytes are gone, so a
// deployment holds no duplicate of every picture it took, and the frame is kept,
// so the srcset and every download find the pixels they were promised.
func TestAnImageUploadThatSucceedsLeavesTheFrameAloneInTheStore(t *testing.T) {
	_, conn := dbtest.Schema(t, file.Migrations)
	dir := t.TempDir()
	store := &keptAside{Storage: internal.NewLocal(dir)}
	svc := internal.NewService(store, 16<<20, 0, 0)

	body := jpegOf(t, 40, 20)
	ctx := tenancy.WithTenant(t.Context(), acme)
	var stored *contracts.File
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		stored, err = svc.Upload(ctx, held(tx), contracts.Upload{
			Name: "frame.jpg", ContentType: "image/jpeg",
			Body: bytes.NewReader(body), Declared: int64(len(body)),
		})
		return err
	}); err != nil {
		t.Fatalf("upload a picture: %v", err)
	}
	if stored.StorageKey == "" {
		t.Fatal("the row names no object")
	}
	got := keysUnder(t, dir)
	if len(got) != 1 {
		t.Fatalf("a good image upload left %v in the store, want the one object its row names", got)
	}
	if got[0] != stored.StorageKey {
		t.Errorf("the store holds %s and the row names %s, want the row to name the byte a reader is served", got[0], stored.StorageKey)
	}
}
