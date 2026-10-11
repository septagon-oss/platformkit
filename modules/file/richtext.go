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
	_, err := r.Service.SetUses(ctx, tx, contracts.Use{
		Module: in.Module, Entity: in.Entity, Record: in.Record, Field: in.Field, Locale: in.Locale,
	}, in.Files)
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
type RichTextFiles struct{ Opener contracts.Opener }

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
		return richtext.Image{Src: imagePath(id, audience), SrcSet: fmt.Sprintf("%s %dw", imagePath(id, audience), f.Width),
			Width: f.Width, Height: f.Height}, nil
	}
	config, format, err := image.DecodeConfig(io.LimitReader(body, 1<<20))
	if err != nil {
		return richtext.Image{}, fmt.Errorf("file: decode image header: %w", err)
	}
	if format != "jpeg" && format != "png" && format != "gif" {
		return richtext.Image{}, richtext.ErrMissing
	}
	if config.Width <= 0 || config.Height <= 0 {
		return richtext.Image{}, richtext.ErrMissing
	}
	return richtext.Image{Src: imagePath(id, audience), SrcSet: fmt.Sprintf("%s %dw", imagePath(id, audience), config.Width), Width: config.Width, Height: config.Height}, nil
}

// imagePath is the one place this adapter writes a URL, so the workspace door
// and the public one cannot drift into two shapes.
func imagePath(id uuid.UUID, audience richtext.Audience) string {
	if audience == richtext.Public {
		return "/api/v1/public/file/files/" + id.String()
	}
	return "/api/v1/file/files/" + id.String() + "/content"
}
