package file

import (
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/richtext"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
)

// RecordUses adapts this module's SetUses to the kit's port, so that a resource
// mounted by kit/rest records its references here in the transaction that wrote
// them. It is the same relationship as RichTextFiles below it: the kit declares
// what it needs, this module satisfies it, and neither imports the other's
// vocabulary.
type RecordUses struct{ Service contracts.Service }

func (r RecordUses) SetUses(ctx context.Context, tx db.Tx[db.Tenant], in rest.UsesInput) error {
	if r.Service == nil {
		return fmt.Errorf("file: no service to record a use with")
	}
	use := contracts.Use{
		Module: in.Module, Entity: in.Entity, Record: in.Record, Field: in.Field, Locale: in.Locale,
	}
	if in.Public {
		// A published record files public rows. The silent version of a Service that
		// cannot be asked that is a published page whose picture nobody can load, so
		// the write is refused and the wiring says so.
		shown, ok := r.Service.(contracts.ShownUses)
		if !ok {
			return fmt.Errorf("file: %T was wired to record a use for a published record and answers no contracts.ShownUses", r.Service)
		}
		_, err := shown.SetUsesShown(ctx, tx, use, in.Files, true)
		return err
	}
	_, err := r.Service.SetUses(ctx, tx, use, in.Files)
	return err
}

// RichTextFiles adapts this module's tenant-scoped opener to the generic image port.
//
// Dimensions come off the row: the upload pass measured the frame it stored, so
// a render costs one read of a row that is already open rather than a fetch of
// the object and a decode of its header — and the numbers are the upright ones,
// because the pass turned the frame before it wrote it. A row that carries 0 x 0
// predates the pass or holds bytes no decoder read, and for those the header is
// still read, so a file uploaded last year renders the size it always did.
//
// Variants is optional and says so by being nil-able: it is the widths this file
// can serve today, and a composition without it gets today's answer — one
// descriptor, the stored frame — which is a srcset of one and not a broken one.
type RichTextFiles struct {
	Opener   contracts.Opener
	Variants contracts.VariantServable
}

// Resolve never turns a foreign, absent, private-public or non-image row into a URL.
func (a RichTextFiles) Resolve(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, audience richtext.Audience) (richtext.Image, error) {
	if a.Opener == nil {
		return richtext.Image{}, fmt.Errorf("file: richtext opener is missing")
	}
	f, body, err := a.Opener.Open(ctx, tx, id, audience == richtext.Public)
	if errors.Is(err, crud.ErrNotFound) {
		return richtext.Image{}, richtext.ErrMissing
	}
	if err != nil {
		return richtext.Image{}, err
	}
	defer body.Close()
	if !strings.HasPrefix(f.ContentType, "image/") || f.ContentType == "image/svg+xml" {
		return richtext.Image{}, richtext.ErrMissing
	}
	if f.Width > 0 && f.Height > 0 {
		return richtext.Image{Src: imagePath(id, audience), SrcSet: a.srcSet(ctx, tx, id, audience, f.Width),
			Width: f.Width, Height: f.Height, Sizes: richtext.ProseSizes}, nil
	}
	config, format, err := image.DecodeConfig(io.LimitReader(body, 1<<20))
	if err != nil {
		return richtext.Image{}, fmt.Errorf("file: decode image header: %w", err)
	}
	if format != "jpeg" && format != "png" && format != "gif" && format != "webp" {
		return richtext.Image{}, richtext.ErrMissing
	}
	if config.Width <= 0 || config.Height <= 0 {
		return richtext.Image{}, richtext.ErrMissing
	}
	return richtext.Image{Src: imagePath(id, audience), SrcSet: a.srcSet(ctx, tx, id, audience, config.Width),
		Width: config.Width, Height: config.Height, Sizes: richtext.ProseSizes}, nil
}

// srcSet is every candidate this file offers a browser, largest first: the
// stored frame at its own width, then each width the encoder has finished, each
// named by the bare address plus the width it stands for.
//
// With nothing ready this is the one-descriptor answer the srcset had before
// widths existed, byte for byte, which is what lets a body written last year and
// an app that wired no variants keep rendering while the job has not been round.
// A candidate wider than the frame is left out rather than refused: it would fail
// the write-time check on the way in, and a read that invented one would be a
// port substituting a promise for a fact.
func (a RichTextFiles) srcSet(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, audience richtext.Audience, frame int) string {
	base := imagePath(id, audience)
	entries := []string{fmt.Sprintf("%s %dw", base, frame)}
	if a.Variants == nil {
		return strings.Join(entries, ", ")
	}
	rows, err := a.Variants.Variants(ctx, tx, id)
	if err != nil {
		// The frame is still offered, and one descriptor is what this port returned
		// before widths existed. What this line does *not* buy is a way through a
		// dead transaction: Postgres aborts a transaction at its first failing
		// statement, so where the read failed that way the caller's own commit fails
		// with it and the page does not render at all — the fallback is the answer for
		// an implementation whose read failed and left the transaction open, and the
		// log line is what names the read that came first once the transaction is
		// already gone. Making this return the error instead was considered and left
		// for a round with the time to decide what every consumer of a missing image
		// does about it; see this task's report, which defers it by name.
		slog.WarnContext(ctx, "file: which widths to offer was not readable", "file", id, "reason", err)
		return strings.Join(entries, ", ")
	}
	// Descending: a candidate list is read largest-first by every browser, and the
	// rows come back smallest-first because that is the order a picture is built
	// in. Walking it backwards is the whole of the sort.
	for i := len(rows) - 1; i >= 0; i-- {
		if rows[i].Width >= frame {
			continue
		}
		entries = append(entries, fmt.Sprintf("%s?w=%d %dw", base, rows[i].Width, rows[i].Width))
	}
	return strings.Join(entries, ", ")
}

// imagePath is the one place this adapter writes a URL, so the workspace door
// and the public one cannot drift into two shapes.
func imagePath(id uuid.UUID, audience richtext.Audience) string {
	if audience == richtext.Public {
		return "/api/v1/public/file/files/" + id.String()
	}
	return "/api/v1/file/files/" + id.String() + "/content"
}

// ShowFiles adapts this module's SetShown to the audience port kit/rest declares,
// for the module that owns a record's public face to call from the command that
// moves the record. The mapping is one way and it loses nothing: a record's face
// covers every field and every locale of it, so the port names no field and the
// Use this module keys its rows by carries the record alone.
type ShowFiles struct{ Service contracts.Service }

func (s ShowFiles) SetShown(ctx context.Context, tx db.Tx[db.Tenant], in rest.ShownInput) error {
	if s.Service == nil {
		return fmt.Errorf("file: no service to assert an audience with")
	}
	return s.Service.SetShown(ctx, tx, contracts.Use{
		Module: in.Module, Entity: in.Entity, Record: in.Record,
	}, in.Files, in.Public)
}
