package main

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/app"
)

func TestAWorkspaceCannotNameAnotherTenantsMark(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	start(t, cfg, c.modules, appOptions(cfg, c, app.All))
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	code, body := putFile(t, cfg, admin, filesPath+"?visibility=public", "mark.svg", "image/svg+xml", "<svg xmlns='http://www.w3.org/2000/svg'/>")
	if code != http.StatusCreated {
		t.Fatalf("upload = %d %s", code, body)
	}
	mark := field(t, body, "id")
	code, body = do(t, cfg, admin, http.MethodPost, acmeHost, tenantPath,
		`{"slug":"globex","name":"Globex","host":"`+globexHost+`"}`)
	if code != http.StatusCreated {
		t.Fatalf("create tenant = %d %s", code, body)
	}
	provision(t, cfg, uuid.MustParse(field(t, body, "id")), "root@globex.localhost")
	globex := signIn(t, cfg, globexHost, "root@globex.localhost", adminPass)
	if code, body := do(t, cfg, globex, http.MethodPut, globexHost, sitePath,
		`{"title":"Globex","logoFileId":"`+mark+`"}`); code != http.StatusOK {
		t.Fatalf("set mark reference = %d %s", code, body)
	}
	face := connectionBody(t, mustBe200(t, cfg, globexHost))
	if _, present := face["logoUrl"]; present {
		t.Errorf("a foreign tenant's file was named: %v", face)
	}
	if face["name"] != "Globex" {
		t.Errorf("omitting a foreign mark lost the workspace name: %v", face)
	}
	if code := statusCode(t, cfg, globexHost, pinnedPublicFile+"/"+mark); code != http.StatusNotFound {
		t.Errorf("foreign host served file with status %d, want 404", code)
	}
	if code := statusCode(t, cfg, acmeHost, pinnedPublicFile+"/"+mark); code != http.StatusOK {
		t.Errorf("owning host lost its public file: %d", code)
	}
}
