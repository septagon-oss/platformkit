package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	admincontracts "github.com/septagon-oss/platformkit/modules/admin/contracts"
	contentcontracts "github.com/septagon-oss/platformkit/modules/content/contracts"
	filecontracts "github.com/septagon-oss/platformkit/modules/file/contracts"
	"github.com/septagon-oss/platformkit/modules/picture"
	picturecontracts "github.com/septagon-oss/platformkit/modules/picture/contracts"
	"github.com/septagon-oss/platformkit/modules/picture/contracts/picturetest"
	"github.com/septagon-oss/platformkit/pkit"
	"github.com/septagon-oss/platformkit/ui/screens"
)

type contentPictureSource struct {
	digest [32]byte
	key    atomic.Value
}

func (s *contentPictureSource) Load(ctx context.Context, tx db.Tx[db.Tenant], key string) (picturecontracts.Input, error) {
	id, err := uuid.Parse(key)
	if err != nil {
		return picturecontracts.Input{}, picturecontracts.ErrUnavailable
	}
	row, err := crud.Get[*contentcontracts.Content](tx, id)
	if errors.Is(err, crud.ErrNotFound) {
		return picturecontracts.Input{}, picturecontracts.ErrUnavailable
	}
	if err != nil {
		return picturecontracts.Input{}, picturecontracts.ErrTemporary
	}
	actor, ok := tenancy.ActorFrom(ctx)
	if !ok || row.AuthorID != actor {
		return picturecontracts.Input{}, picturecontracts.ErrUnavailable
	}
	return picturecontracts.Input{TemplateDigest: s.digest, Value: picturetest.Value()}, nil
}
func TestComposedPictureUsesTheGeneratedListAndSessionDoor(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	template, err := picturecontracts.Compile("fixture", picturetest.SVG)
	if err != nil {
		t.Fatal(err)
	}
	source := &contentPictureSource{digest: template.Digest()}
	source.key.Store("")
	fixture := pkit.NewModule("picturefixture", func(w *pkit.Wiring) (module.Module, error) {
		pkit.Put[picturecontracts.Source](w, source)
		pkit.Put[picturecontracts.Templates](w, picturecontracts.Templates{template})
		return module.Module{Name: "picturefixture", Routes: func(s httpx.Surfaces) {
			s.RegisterResource(httpx.Resource{Module: "picturefixture", Entity: "picture", Path: "/pictures", Screen: "/app/picturefixture/pictures", Read: contentcontracts.PermissionContentRead, Operations: []httpx.CRUD{httpx.CRUDList}, Present: entity.EntryHints{Plural: "Pictures", PrimaryField: "title"}, Schema: entity.Schema{Module: "picturefixture", Entity: "picture", Path: "/api/v1/picturefixture/pictures", Fields: []entity.Field{{Name: "title", Type: entity.TypeString}, {Name: "picture", Type: entity.TypePicture, ReadOnly: true, Present: "image"}}}, List: func(ctx context.Context, _ crud.Query) ([]map[string]any, int64, error) {
				tx, ok := httpx.TxFrom(ctx)
				if !ok {
					return nil, 0, rest.Fault(picturecontracts.ErrTemporary)
				}
				key := source.key.Load().(string)
				if _, err := source.Load(ctx, tx, key); err != nil {
					return nil, 0, rest.Fault(crud.ErrNotFound)
				}
				reference, err := entity.ComposedPicture(key)
				if err != nil {
					return nil, 0, err
				}
				return []map[string]any{{"id": key, "title": "Geometry", "picture": reference}}, 1, nil
			}})
		}}, nil
	}, pkit.Provides[picturecontracts.Source](), pkit.Provides[picturecontracts.Templates]())
	application, _, _, _, grants, _, _ := sentence(cfg, catalogues(), newLatePorts(), "product")
	plan, err := application.Use(productPictures(cfg, grants, newLatePorts(), admincontracts.Pictures{Source: filecontracts.PictureSource, ComposedSource: picturecontracts.PictureSource}), fixture, picture.Module).Plan(pkit.Deployment{Environment: pkit.Development, Config: cfg, Inputs: deploymentInputs(cfg), Transports: transports(), Caches: caches()})
	if err != nil {
		t.Fatal(err)
	}
	grants.get = func() []tenancy.Grant { return module.Grants(plan.Modules()) }
	options := plan.Options()
	options.Role = app.All
	options.Transports = transports()
	options.Caches = caches()
	options.Transport = memory.New()
	options.Log = quiet()
	builder := documentBuilder(new(documentGate))
	options.WorkspaceCatalog = app.WorkspaceCatalogRoute(func(ctx context.Context, resources []httpx.Resource) (*screens.Catalog, error) {
		doc, err := builder(ctx, resources)
		if err == nil {
			doc.Pictures.ComposedSource = picturecontracts.PictureSource
		}
		return doc, err
	})
	start(t, cfg, plan.Modules(), options)
	who := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	code, body := do(t, cfg, who, "POST", acmeHost, contentPath, `{"slug":"picture-fixture","title":"Geometry","kind":"page"}`)
	if code != 201 {
		t.Fatalf("source create %d %s", code, body)
	}
	key := field(t, body, "id")
	source.key.Store(key)
	token, _ := entity.ComposedPicture(key)
	door := strings.ReplaceAll(picturecontracts.PictureSource, "{reference}", string(token))
	code, body = do(t, cfg, who, "GET", acmeHost, "/app/picturefixture/pictures", "")
	if code != 200 || !strings.Contains(body, `src="`+door+`"`) || strings.Contains(body, ">"+string(token)+"<") {
		t.Fatalf("generated list %d %s", code, body)
	}
	code, catalog := do(t, cfg, who, "GET", acmeHost, "/api/v1/app/resources", "")
	if code != 200 || !strings.Contains(catalog, `"composedSource":"`+picturecontracts.PictureSource+`"`) || !strings.Contains(catalog, `"type":"picture"`) {
		t.Fatalf("picture catalog %d %s", code, catalog)
	}
	code, body = do(t, cfg, who, "GET", acmeHost, door, "")
	if code != 200 || body != picturetest.Oracle {
		t.Fatalf("picture GET %d %s", code, body)
	}
	if gate := os.Getenv("PLATFORMKIT_PICTURE_DESIGN_GATE"); gate != "" {
		target, err := url.Parse("http://" + cfg.Server.Addr)
		if err != nil {
			t.Fatal(err)
		}
		proxy := httputil.NewSingleHostReverseProxy(target)
		original := proxy.Director
		proxy.Director = func(r *http.Request) {
			original(r)
			r.Host = acmeHost
			for _, cookie := range who.Jar.Cookies(target) {
				r.AddCookie(cookie)
			}
		}
		preview := httptest.NewServer(proxy)
		command := exec.CommandContext(t.Context(), "python3", gate, preview.URL+"/app/picturefixture/pictures", "--widths", "390,1440")
		output, err := command.CombinedOutput()
		preview.Close()
		t.Logf("design floor command: python3 %s %s/app/picturefixture/pictures --widths 390,1440\n%s", gate, preview.URL, output)
		if err != nil {
			t.Fatal(err)
		}
	}
	if browser := os.Getenv("PLATFORMKIT_PICTURE_BROWSER"); browser != "" {
		configPath, err := filepath.Abs("../../e2e/picture.config.ts")
		if err != nil {
			t.Fatal(err)
		}
		command := exec.CommandContext(t.Context(), browser, "test", "--config", configPath, "--output", t.TempDir())
		command.Env = append(os.Environ(), "PLATFORMKIT_E2E_URL=http://"+acmeHost+":"+strings.Split(cfg.Server.Addr, ":")[1], "PLATFORMKIT_E2E_EMAIL="+adminEmail, "PLATFORMKIT_E2E_PASSWORD="+adminPass, "PLATFORMKIT_PICTURE_KEY="+key, "PLATFORMKIT_PICTURE_DOOR="+door)
		output, err := command.CombinedOutput()
		t.Logf("picture browser journey:\n%s", output)
		if err != nil {
			t.Fatal(err)
		}
	} else {
		code, body = do(t, cfg, who, "DELETE", acmeHost, contentPath+"/"+key, "")
		if code != 204 {
			t.Fatalf("source deletion %d %s", code, body)
		}
		code, catalog := do(t, cfg, who, "GET", acmeHost, "/api/v1/app/resources", "")
		if code != 200 || !strings.Contains(catalog, `"composedSource":"`+picturecontracts.PictureSource+`"`) || !strings.Contains(catalog, `"type":"picture"`) {
			t.Fatalf("picture catalog %d %s", code, catalog)
		}
		code, body = do(t, cfg, who, "GET", acmeHost, door, "")
		if code != 404 || strings.Contains(body, "<svg") {
			t.Fatalf("deleted warm source %d %s", code, body)
		}
	}
}
