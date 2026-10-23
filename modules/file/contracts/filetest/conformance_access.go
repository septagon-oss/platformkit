package filetest

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
)

func concealPrivateFile(t *testing.T, f Fixture) {
	row := stored(t, f, hello)
	// Not forbidden: a caller who is not signed in learns nothing about
	// what this tenant has, including whether it has this.
	if _, _, err := f.Service.Open(f.Ctx, f.Tx, row.ID, true); !errors.Is(err, crud.ErrNotFound) {
		t.Errorf("a private file at the public door = %v, want ErrNotFound", err)
	}
}

func servePublicFile(t *testing.T, f Fixture) {
	row, err := f.Service.Upload(f.Ctx, f.open, upload("logo.png", "image/png", contracts.VisibilityPublic, png))
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	got, back := read(t, f, row.ID, true)
	if back != png || got.ContentType != "image/png" {
		t.Errorf("the public file read back as %q/%q", got.ContentType, back)
	}
}

func deleteFile(t *testing.T, f Fixture) {
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
}

func refuseUnknownFile(t *testing.T, f Fixture) {
	id := uuid.New()
	if _, _, err := f.Service.Open(f.Ctx, f.Tx, id, false); !errors.Is(err, crud.ErrNotFound) {
		t.Errorf("Open of an unknown file = %v, want ErrNotFound", err)
	}
	if _, err := f.Service.Delete(f.Ctx, f.Tx, id); !errors.Is(err, crud.ErrNotFound) {
		t.Errorf("Delete of an unknown file = %v, want ErrNotFound", err)
	}
}
