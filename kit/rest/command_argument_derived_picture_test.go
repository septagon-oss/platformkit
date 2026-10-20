package rest_test

import (
	"context"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/rest"
)

// A read-only picture is derived by the server from its row, so a command
// argument cannot carry one; a caller-owned argument on a resource whose own
// picture is immutable still registers with its fields in order.
func TestACommandArgumentCannotCarryADerivedPicture(t *testing.T) {
	t.Run("value", func(t *testing.T) {
		refuseCommandArgument[struct {
			Picture entity.Picture `json:"picture" readOnly:"true"`
		}](t, rest.CommandOptions{}, "picture")
	})
	t.Run("nullable", func(t *testing.T) {
		refuseCommandArgument[struct {
			Reason  string          `json:"reason"`
			Picture *entity.Picture `json:"picture" readOnly:"true"`
		}](t, rest.CommandOptions{}, "picture")
	})
	t.Run("caller-owned on a picture resource", func(t *testing.T) {
		type condition struct {
			IfRevision int64  `json:"ifRevision"`
			Reason     string `json:"reason"`
		}
		pictures := rest.Spec[*computedPictureRow]{Module: "computed", Entity: "computed", Path: "/pictures",
			Read: "computed:read", Write: "computed:write", Immutable: []string{"picture", "nullable"}}
		api, _ := patchDocumentAPI()
		surfaces := api.Surfaces(pictures.Module)
		pictures.Mount(surfaces)
		rest.Command(surfaces, pictures, "confirm", "Confirm", "Confirm the picture's row.", nil,
			func(context.Context, db.Tx[db.Tenant], uuid.UUID, condition) (*computedPictureRow, error) {
				return nil, nil
			},
			rest.CommandOptions{})
		resources := api.Resources()
		if len(resources) != 1 || len(resources[0].Commands) != 1 {
			t.Fatalf("resources = %v, want one resource with confirm", resources)
		}
		var got []string
		for _, f := range resources[0].Commands[0].Fields {
			got = append(got, f.Name)
		}
		if want := []string{"ifRevision", "reason"}; !slices.Equal(got, want) {
			t.Errorf("fields = %v, want %v", got, want)
		}
		if f, ok := entity.FieldNamed(pictures.Schema().Fields, "picture"); !ok || f.Type != entity.TypePicture || !f.ReadOnly {
			t.Errorf("resource picture = %+v, want a read-only picture", f)
		}
		recorded(t, api, "computed-computed-confirm")
	})
}
