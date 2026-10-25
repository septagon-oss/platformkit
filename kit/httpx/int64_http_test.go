package httpx_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/septagon-oss/platformkit/kit/httpx"
)

type exactIntegerBody struct {
	Amount int64  `json:"amount" minimum:"9007199254740993" maximum:"9007199254740995" example:"9007199254740993"`
	Count  int32  `json:"count" minimum:"1"`
	Title  string `json:"title" minLength:"2"`
}

type exactIntegerReply exactIntegerBody

func integerRequest(router http.Handler, method, path, body, accept, contentType string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://"+host+path, strings.NewReader(body))
	r.Header.Set("Accept", accept)
	r.Header.Set("Content-Type", contentType)
	r.AddCookie(&http.Cookie{Name: httpx.SessionCookie, Value: "present"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	return w
}

func exactHTTP(t *testing.T) (*httpx.API, http.Handler, *atomic.Int32) {
	t.Helper()
	api, router, f := setupWith(t, true)
	f.allow = true
	f.signedIn()
	calls := new(atomic.Int32)
	httpx.Register(api.Surfaces(probe).App, huma.Operation{OperationID: "exact-integer", Method: http.MethodPost, Path: "/exact"}, httpx.Permission("probe:write"),
		func(_ context.Context, in *struct{ Body exactIntegerBody }) (*struct{ Body exactIntegerReply }, error) {
			calls.Add(1)
			return &struct{ Body exactIntegerReply }{exactIntegerReply(in.Body)}, nil
		})
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatal(err)
	}
	return api, router, calls
}

func TestTypedIntegerHTTPAcceptsBothFormsAndKeepsLegacyOutput(t *testing.T) {
	api, router, calls := exactHTTP(t)
	for _, amount := range []string{`9007199254740993`, `"9007199254740993"`, `90071992547409930e-1`} {
		for _, accept := range []string{"", "*/*", "application/json", "application/vnd.platformkit.int64+json"} {
			got := integerRequest(router, http.MethodPost, at(api, "/exact"), `{"amount":`+amount+`,"count":7,"title":"ok"}`, accept, "application/vnd.platformkit.int64+json")
			integerStatus(t, got, 200)
			var body map[string]json.RawMessage
			if err := json.Unmarshal(got.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			want, media := `9007199254740993`, "application/json"
			if accept == "application/vnd.platformkit.int64+json" {
				want, media = `"9007199254740993"`, "application/vnd.platformkit.int64+json"
			}
			if string(body["amount"]) != want || string(body["count"]) != "7" {
				t.Fatalf("body = %s", got.Body.String())
			}
			if got.Header().Get("Content-Type") != media {
				t.Errorf("Content-Type = %s", got.Header().Get("Content-Type"))
			}
			if !strings.Contains(strings.Join(got.Header().Values("Vary"), ","), "Accept") {
				t.Error("missing Vary: Accept")
			}
			if accept == "application/vnd.platformkit.int64+json" && !strings.Contains(string(body["$schema"]), "Int64Response.json") {
				t.Errorf("schema link = %s", body["$schema"])
			}
		}
	}
	if calls.Load() != 12 {
		t.Fatal(calls.Load())
	}
}

func TestMalformedIntegerHTTPNeverInvokesTheHandler(t *testing.T) {
	api, router, calls := exactHTTP(t)
	for _, amount := range []string{`9007199254740992`, `"9007199254740992"`, `9007199254740996`, `"9007199254740996"`, `9007199254740993.1`, `1e999999999`, `null`, `true`, `[]`, `{}`, `"01"`, `"+1"`, `"1e3"`, `"9223372036854775808"`} {
		got := integerRequest(router, http.MethodPost, at(api, "/exact"), `{"amount":`+amount+`,"count":7,"title":"changed"}`, "application/vnd.platformkit.int64+json", "application/json")
		integerStatus(t, got, 422)
		if !strings.Contains(got.Body.String(), "amount") {
			t.Errorf("refusal omitted field: %s", got.Body.String())
		}
	}
	for _, body := range []string{`{"amount":9007199254740993,"count":0,"title":"ok"}`, `{"amount":9007199254740993,"count":7,"title":"x"}`} {
		integerStatus(t, integerRequest(router, http.MethodPost, at(api, "/exact"), body, "application/vnd.platformkit.int64+json", "application/json"), 422)
	}
	for _, body := range []string{"", `{`, `{"amount":01}`, `{} {}`} {
		integerStatus(t, integerRequest(router, http.MethodPost, at(api, "/exact"), body, "application/vnd.platformkit.int64+json", "application/json"), 400)
	}
	if calls.Load() != 0 {
		t.Fatalf("invalid body reached handler %d times", calls.Load())
	}
}

func TestIntegerOpenAPIViewsDescribeTheirOwnRepresentations(t *testing.T) {
	api, router, _ := exactHTTP(t)
	for _, query := range []string{"", "?int64=string"} {
		got := integerRequest(router, http.MethodGet, "/openapi.json"+query, "", "", "application/json")
		if got.Code != 200 || got.Header().Get("Content-Type") != "application/openapi+json" {
			t.Fatalf("document = %d %s", got.Code, got.Body.String())
		}
		var doc struct {
			Paths map[string]struct {
				Post struct {
					RequestBody struct{ Content map[string]json.RawMessage }
					Responses   map[string]struct{ Content map[string]json.RawMessage }
				}
			}
			Components struct{ Schemas map[string]json.RawMessage }
		}
		if err := json.Unmarshal(got.Body.Bytes(), &doc); err != nil {
			t.Fatal(err)
		}
		op := doc.Paths[at(api, "/exact")].Post
		if query == "" && len(op.RequestBody.Content) != 1 {
			t.Fatal("frozen input gained a required-media signature")
		}
		if query != "" && len(op.RequestBody.Content) != 2 {
			t.Fatal("selected input does not describe both media")
		}
		if len(op.Responses["200"].Content["application/vnd.platformkit.int64+json"]) == 0 {
			t.Fatal("missing decimal response")
		}
		var numeric, decimal struct {
			Properties map[string]struct {
				Type   string
				Format string
			}
		}
		if err := json.Unmarshal(doc.Components.Schemas["ExactIntegerBody"], &numeric); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(doc.Components.Schemas["ExactIntegerReplyInt64Response"], &decimal); err != nil {
			t.Fatal(err)
		}
		if numeric.Properties["amount"].Type != "integer" || decimal.Properties["amount"].Type != "string" || decimal.Properties["amount"].Format != "int64" || decimal.Properties["count"].Type != "integer" {
			t.Fatal("incorrect scalar projection")
		}
		if query != "" && !strings.Contains(string(doc.Components.Schemas["ExactIntegerBodyInt64Input"]), `9007199254740993`) {
			t.Fatalf("projection rounded the bound/example: %s", doc.Components.Schemas["ExactIntegerBodyInt64Input"])
		}
	}
	for _, query := range []string{"?int64=number", "?int64=string&int64=string", "?int64="} {
		integerStatus(t, integerRequest(router, http.MethodGet, "/openapi.json"+query, "", "", "application/json"), 400)
	}
}

func integerStatus(t *testing.T, got *httptest.ResponseRecorder, want int) {
	t.Helper()
	if got.Code != want {
		t.Fatalf("status = %d, want %d: %s", got.Code, want, got.Body.String())
	}
}
