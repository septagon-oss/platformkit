package rest_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
)

type computedPictureRow struct {
	crud.Base
	Title    string          `json:"title"`
	Picture  entity.Picture  `json:"picture" readOnly:"true" ui:"present:image" gorm:"-"`
	Nullable *entity.Picture `json:"nullable" readOnly:"true" ui:"present:image" gorm:"-"`
}

func (computedPictureRow) TableName() string { return "rest_tasks" }
func TestDerivedPictureSchemaAndMountContract(t *testing.T) {
	api, router := patchDocumentAPI()
	spec := rest.Spec[*computedPictureRow]{Module: "computed", Entity: "computed", Path: "/pictures", Name: rest.Name{One: "Computed", Other: "Computed"}, Read: "computed:read", Write: "computed:write", Immutable: []string{"picture", "nullable"}}
	spec.Mount(api.Surfaces("computed"))
	fields := spec.Schema().Fields
	for _, name := range []string{"picture", "nullable"} {
		f, ok := entity.FieldNamed(fields, name)
		if !ok || f.Type != entity.TypePicture || !f.ReadOnly {
			t.Fatalf("field %s = %+v", name, f)
		}
	}
	schema := patchComponents(t, router)["ComputedPictureRow"]
	if schema == nil {
		t.Fatal("missing entity schema")
	}
	for _, name := range []string{"picture", "nullable"} {
		p := schema.Properties[name]
		if p == nil || (fmt.Sprint(p.Type) != "string" && fmt.Sprint(p.Type) != "[string null]") || p.MaxLength == nil || *p.MaxLength != 686 || !p.ReadOnly {
			t.Fatalf("picture wire schema %s = %+v", name, p)
		}
	}
	// Read-only resources require no immutable guard for writes they do not mount.
	readOnly := spec
	readOnly.Operations = []httpx.CRUD{httpx.CRUDList, httpx.CRUDRead}
	readOnly.Immutable = nil
	other, _ := patchDocumentAPI()
	readOnly.Mount(other.Surfaces("computed"))
	readOnly.Operations = []httpx.CRUD{httpx.CRUDList, httpx.CRUDRead, httpx.CRUDDelete}
	deleteOnly, _ := patchDocumentAPI()
	readOnly.Mount(deleteOnly.Surfaces("computed"))
	bad := spec
	bad.Immutable = []string{"picture"}
	func() {
		defer func() {
			p := recover()
			if p == nil || !strings.Contains(p.(string), "nullable") || !strings.Contains(p.(string), "Immutable") {
				t.Errorf("missing immutable guard refusal %v", p)
			}
		}()
		bad.Mount(httpx.Surfaces{})
	}()
}

func TestGenericWritesRefusePictureReferencesWithoutPartialWrites(t *testing.T) {
	s := rest.Spec[*computedPictureRow]{Module: "computed", Entity: "computed", Path: "/pictures", Name: rest.Name{One: "Computed", Other: "Computed"}, Read: "computed:read", Write: "computed:write", Immutable: []string{"picture", "nullable"}}
	_, router, admin := mountAs(t, s, caller{})
	token, _ := entity.ComposedPicture("source")
	for _, body := range []string{`{"title":"refused","picture":"` + string(token) + `"}`, `{"title":"refused","Picture":null}`, `{"title":"refused","nullable":null}`} {
		code, reply := call(t, router, http.MethodPost, "/api/v1/computed/pictures", body)
		if code != 422 {
			t.Fatalf("create accepted picture %d %s", code, reply)
		}
	}
	var count int
	if err := admin.QueryRowContext(t.Context(), `SELECT count(*) FROM rest_tasks`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("refused create wrote rows: %d %v", count, err)
	}
	code, reply := call(t, router, http.MethodPost, "/api/v1/computed/pictures", `{"title":"Original"}`)
	if code != 201 {
		t.Fatalf("create %d %s", code, reply)
	}
	rowID := id(t, reply)
	var before int
	if err := admin.QueryRowContext(t.Context(), `SELECT count(*) FROM platformkit_outbox`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	code, reply = call(t, router, http.MethodPatch, "/api/v1/computed/pictures/"+rowID, `{"title":"Partial write","picture":"`+string(token)+`"}`)
	if code != 422 {
		t.Fatalf("patch accepted picture %d %s", code, reply)
	}
	var title string
	if err := admin.QueryRowContext(t.Context(), `SELECT title FROM rest_tasks WHERE id=$1`, rowID).Scan(&title); err != nil || title != "Original" {
		t.Fatalf("partial write %q %v", title, err)
	}
	if err := admin.QueryRowContext(t.Context(), `SELECT count(*) FROM platformkit_outbox`).Scan(&count); err != nil || count != before {
		t.Fatalf("refusal emitted event: %d versus %d %v", count, before, err)
	}
}
