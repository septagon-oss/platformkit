package file_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	pngimage "image/png"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/richtext"
	"github.com/septagon-oss/platformkit/kit/richtext/richtexttest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/file"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
)

func TestRichTextFilesConformAcrossTwoTenants(t *testing.T) {
	_, conn := dbtest.Schema(t, file.Migrations)
	a := tenancy.Tenant{ID: uuid.New(), Slug: "a"}
	b := tenancy.Tenant{ID: uuid.New(), Slug: "b"}
	service, _ := file.Module(file.Deps{Storage: file.Local(t.TempDir())})
	var pngBytes bytes.Buffer
	if err := pngimage.Encode(&pngBytes, image.NewRGBA(image.Rect(0, 0, 2, 3))); err != nil {
		t.Fatal(err)
	}
	var id uuid.UUID
	if err := db.Run(tenancy.WithTenant(t.Context(), a), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		row, err := service.Upload(ctx, func(context.Context) (db.Tx[db.Tenant], error) { return tx, nil }, contracts.Upload{
			Name: "image.png", ContentType: "image/png", Visibility: contracts.VisibilityPrivate, Declared: int64(pngBytes.Len()), Body: bytes.NewReader(pngBytes.Bytes()),
		})
		if err == nil {
			id = row.ID
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/file/files/" + id.String() + "/content"
	fake := &richtexttest.FakeFiles{}
	fake.Put(a, id, richtext.Image{Src: path, SrcSet: path + " 2w", Width: 2, Height: 3}, false)
	for name, port := range map[string]richtext.Files{"fake": fake, "SQL": file.RichTextFiles{Opener: service}} {
		t.Run(name, func(t *testing.T) {
			for _, tc := range []struct {
				tenant   tenancy.Tenant
				audience richtext.Audience
				found    bool
			}{
				{a, richtext.Workspace, true}, {a, richtext.Public, false}, {b, richtext.Workspace, false}, {b, richtext.Public, false},
			} {
				err := db.Run(tenancy.WithTenant(t.Context(), tc.tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
					image, err := port.Resolve(ctx, tx, id, tc.audience)
					if tc.found {
						if err != nil || image.Src != path || image.Width != 2 || image.Height != 3 {
							t.Errorf("own image = %+v, %v", image, err)
						}
						if _, err := richtext.Prepare(ctx, tx, "![alt](pk-file:"+id.String()+")", port, 100); err != nil {
							t.Errorf("own write: %v", err)
						}
					} else {
						if !errors.Is(err, richtext.ErrMissing) {
							t.Errorf("other/public image = %+v, %v", image, err)
						}
						if tc.tenant == b {
							_, writeErr := richtext.Prepare(ctx, tx, "![alt](pk-file:"+id.String()+")", port, 100)
							var refused *richtext.Refused
							if !errors.As(writeErr, &refused) || !strings.Contains(writeErr.Error(), "missing image") {
								t.Errorf("foreign write: %v", writeErr)
							}
						}
					}
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
