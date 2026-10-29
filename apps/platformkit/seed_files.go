package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"mime"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/seed"
	filecontracts "github.com/septagon-oss/platformkit/modules/file/contracts"
)

// fileSeeder seeds the one image a demonstration carries. The bytes live beside
// the record that names them, in the same embedded tree the records came from, and
// they go through the file module's own Upload — the door the upload form uses —
// so a seeded file is counted, hashed, quota-checked, given the visibility its
// record declares, and announced by file.uploaded exactly as an upload is. No row
// is written by hand here.
type fileSeeder struct{ svc filecontracts.Service }

func (fileSeeder) Resource() seed.Resource {
	return seed.Resource{
		Alias: "files", Module: "file", Entity: "file",
		// No natural key: a file's identity is its id, and the name a person reads
		// is not unique. The seed finds its own row through seed_keys alone.
		WriteGrant: filecontracts.PermissionFileManage,
		// No delete path: the bytes behind a file may be read by something that
		// linked to them, and a YAML record that stopped existing is not the event
		// that decides they should go.
		Prunable: false,
	}
}

// A file record's bytes live beside the document that names them. The loader
// resolves `asset` against its own document, refuses a path that leaves the tree,
// and Record.AssetBytes reads what that path names out of the filesystem the
// records were loaded from — so the bytes a record uploads are the bytes that
// record points at, and a run that read its records from one tree can never upload
// out of another. The name the upload carries is the last part of the path the
// record named, the only part a reader of the file can see.
func (fileSeeder) Target(_ context.Context, r seed.Record, _ map[string]uuid.UUID, _ time.Time) (seed.Target, error) {
	if r.Asset == "" {
		return seed.Target{}, errors.New("a file record names the asset beside it")
	}
	// Read them here, at the record's own line: bytes that have gone between
	// loading and applying refuse this record, not the upload.
	body, err := r.AssetBytes()
	if err != nil {
		return seed.Target{}, err
	}
	name := path.Base(r.Asset)
	text, err := seedTexts(r, "name", "contentType", "visibility")
	if err != nil {
		return seed.Target{}, err
	}
	if declared := text["name"]; declared != "" && declared != name {
		return seed.Target{}, fmt.Errorf("a file record's key names its asset %q, not %q", name, declared)
	}
	// The media type is the name's, because the name is the record's own answer
	// about the upload. A record that also states one must state the same type:
	// reading a declared contentType and then storing the extension's answer would
	// be reading the field and throwing it away.
	contentType := mime.TypeByExtension(strings.ToLower(path.Ext(name)))
	if declared := text["contentType"]; declared != "" && declared != contentType {
		return seed.Target{}, fmt.Errorf("a file record's asset %q has media type %q, not %q", name, contentType, declared)
	}
	// Visibility travels to the owner's Upload. Which reader a stored file answers
	// is the record's own declaration, and the module's two visibilities are the
	// only answers: a seed that named one of them for every record would hand an
	// anonymous reader the bytes a file declared private. Public answers a record
	// that wrote no visibility, and never one this run could not read — a record
	// whose `visibility: 0` was read as absent would be a private upload that
	// anybody can open, keyed and reported as settled.
	visibility, given := text["visibility"]
	if !given {
		visibility = filecontracts.VisibilityPublic
	}
	if visibility != filecontracts.VisibilityPublic && visibility != filecontracts.VisibilityPrivate {
		return seed.Target{}, fmt.Errorf("visibility %q is not %s or %s", visibility,
			filecontracts.VisibilityPublic, filecontracts.VisibilityPrivate)
	}
	// Two values are create-only rather than state, and the file module is the reason
	// for the second. The bytes are the first: an upload writes them once and no rerun
	// patches them (see Update), so they instruct the record's creation the way a
	// relative date does. The media type is the second because the owner does not keep
	// what the record declared: a raster is measured, turned and re-encoded on its way
	// into storage, and the frame's alpha decides the container (decision 0069 §4). So
	// a stored row's digest is the digest of the frame a reader is served, and its media
	// type the container those pixels chose.
	//
	// A record that reconciled either would refuse every image on the run after the one
	// that wrote it, over a row whose bytes no seed run could put there in any case. What
	// it compares is the two facts the owner keeps as declared: the name a list shows and
	// the visibility an Open answers. Both create-only values stay named, because a
	// declared field is accounted for or refused.
	return seed.Target{
		Fields:     map[string]any{"name": name, "visibility": visibility},
		CreateOnly: map[string]any{"asset": body, "contentType": contentType},
	}, nil
}

func (fileSeeder) Read(ctx context.Context, tx db.Tx[db.Tenant], key seed.Key, _ bool) (seed.Snapshot, error) {
	if key.RecordID == uuid.Nil {
		// Without provenance there is nothing to read: a name is not an identity
		// here, and guessing which of a tenant's files "looks like" a seeded one
		// would be a seed that deleted a person's upload.
		return seed.Snapshot{}, nil
	}
	var (
		row *filecontracts.File
		err error
	)
	row, err = crud.Get[*filecontracts.File](tx, key.RecordID)
	if errors.Is(err, crud.ErrNotFound) {
		return seed.Snapshot{}, nil
	}
	if err != nil {
		return seed.Snapshot{}, err
	}
	return fileState(row), nil
}

func (w fileSeeder) Create(ctx context.Context, tx db.Tx[db.Tenant], t seed.Target) (seed.Snapshot, error) {
	name := t.Fields["name"].(string)
	body, uploaded := t.CreateOnly["asset"].([]byte)
	if !uploaded {
		return seed.Snapshot{}, fmt.Errorf("the record behind %q names no asset bytes to upload", name)
	}
	// The media type is create-only too (see Target): it is what this upload
	// declares, and the owner answers with the container the frame re-encodes to.
	contentType, declared := t.CreateOnly["contentType"].(string)
	if !declared {
		return seed.Snapshot{}, fmt.Errorf("the record behind %q names no media type for its asset", name)
	}
	// The accessor answers with the run's own transaction, because there is
	// nothing to stream: the bytes are already in the binary, so no connection
	// stands open while they arrive, which is the only reason Upload takes an
	// accessor rather than a transaction.
	row, err := w.svc.Upload(ctx, func(context.Context) (db.Tx[db.Tenant], error) { return tx, nil },
		filecontracts.Upload{
			Name: name, ContentType: contentType,
			Visibility: t.Fields["visibility"].(string), Declared: -1, Body: bytes.NewReader(body),
		})
	if err != nil {
		return seed.Snapshot{}, err
	}
	return fileState(row), nil
}

// fileState is the record as its owner stores it, in the two facts the owner keeps as
// they were declared: the name a list shows and the visibility that decides which
// reader an Open answers. Read, Create and the rerun that finds them both equal come
// through here, because the two sides of the comparison must be the same facts in the
// same spellings — and a snapshot that left the visibility out would call a record the
// file declared private unchanged while it sat in a public row.
//
// Neither the digest nor the media type the row carries is among them, and the file
// module's image pass is why (decision 0069 §4): both belong to the frame the module
// stored rather than to what the record declared, so neither is a value this record
// could reconcile without refusing the run that found nothing to write. Target says
// which two create-only instructions the record does send.
func fileState(row *filecontracts.File) seed.Snapshot {
	return seed.Snapshot{Present: true, ID: row.ID,
		Fields: map[string]any{"name": row.Name, "visibility": row.Visibility}}
}

// Update refuses, and the refusal is the design: a rerun reads back the name and the
// visibility, finds them unchanged, and never reaches here. The alternative —
// re-uploading the asset every run — would leave a new row and new bytes on every
// deploy, and the old ones behind. Reaching here means the record and the row disagree
// about one of the two facts the record manages, and no seed command puts new bytes
// behind a row that already has some: the name and the visibility of an existing
// upload are a person's file's, and moving them is that person's own write through the
// module's own screen.
func (fileSeeder) Update(_ context.Context, _ db.Tx[db.Tenant], _ seed.Snapshot, _ seed.Target) (seed.Snapshot, error) {
	return seed.Snapshot{}, errors.New("file: the seed uploads an asset and writes no bytes over it")
}

func (fileSeeder) Delete(context.Context, db.Tx[db.Tenant], seed.Snapshot) error {
	return errors.New("file: a seeded upload is a person's file from the moment it exists")
}
