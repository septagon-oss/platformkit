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
	"github.com/septagon-oss/platformkit/kit/richtext"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
)

// RichTextFiles adapts this module's tenant-scoped opener to the generic image port.
// Original-source dimensions are read from a bounded header until T-0188 records variants.
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
	path := "/api/v1/file/files/" + id.String() + "/content"
	if audience == richtext.Public {
		path = "/api/v1/public/file/files/" + id.String()
	}
	return richtext.Image{Src: path, SrcSet: fmt.Sprintf("%s %dw", path, config.Width), Width: config.Width, Height: config.Height}, nil
}
