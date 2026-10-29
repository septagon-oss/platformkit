package httpx_test

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/septagon-oss/platformkit/kit/httpx"
)

type integerDefaultsBody struct {
	Amount   int64   `json:"amount,omitempty" default:"9007199254740993"`
	Optional *int64  `json:"optional,omitempty" nullable:"true"`
	Values   []int64 `json:"values,omitempty"`
}

func TestTypedIntegerDefaultsAndNullableValuesKeepTheirMeaning(t *testing.T) {
	api, router, f := setup(t)
	f.signedIn()
	f.allow = true
	var values []integerDefaultsBody
	httpx.Register(api.Surfaces(probe).App, huma.Operation{OperationID: "integer-defaults", Method: "POST", Path: "/integer-defaults"}, httpx.Permission("probe:write"),
		func(_ context.Context, in *struct{ Body integerDefaultsBody }) (*struct{ Body integerDefaultsBody }, error) {
			values = append(values, in.Body)
			return &struct{ Body integerDefaultsBody }{in.Body}, nil
		})
	for _, body := range []string{`{}`, `{"amount":"9007199254740995","optional":null,"values":[]}`, `{"optional":"9223372036854775807","values":["-9223372036854775808"]}`} {
		got := integerRequest(router, "POST", at(api, "/integer-defaults"), body, "application/vnd.platformkit.int64+json", "application/json")
		integerStatus(t, got, 200)
	}
	if values[0].Amount != 9007199254740993 || values[0].Optional != nil || values[0].Values != nil {
		t.Fatal(values[0])
	}
	if values[1].Amount != 9007199254740995 || values[1].Optional != nil || values[1].Values == nil {
		t.Fatal(values[1])
	}
	if *values[2].Optional != 9223372036854775807 || values[2].Values[0] != -9223372036854775808 {
		t.Fatal(values[2])
	}
}

func TestDeclaredIntegerNullabilityIsEnforced(t *testing.T) {
	api, router, f := setup(t)
	f.signedIn()
	f.allow = true
	type body struct {
		Required *int64 `json:"required" nullable:"false"`
		Nullable int64  `json:"nullable" nullable:"true"`
	}
	calls := 0
	httpx.Register(api.Surfaces(probe).App, huma.Operation{OperationID: "integer-nullability", Method: "POST", Path: "/integer-nullability"}, httpx.Permission("probe:write"),
		func(_ context.Context, in *struct{ Body body }) (*struct{ Body body }, error) {
			calls++
			return &struct{ Body body }{in.Body}, nil
		})
	got := integerRequest(router, "POST", at(api, "/integer-nullability"), `{"required":null,"nullable":0}`, "application/vnd.platformkit.int64+json", "application/json")
	if got.Code != 422 || calls != 0 {
		t.Fatalf("nonnullable = %d; calls=%d; %s", got.Code, calls, got.Body.String())
	}
	got = integerRequest(router, "POST", at(api, "/integer-nullability"), `{"required":"9007199254740993","nullable":null}`, "application/vnd.platformkit.int64+json", "application/json")
	if got.Code != 200 || calls != 1 {
		t.Fatalf("nullable = %d; calls=%d; %s", got.Code, calls, got.Body.String())
	}
}

type integerProtocol struct {
	Amount int64 `json:"amount"`
}

func (integerProtocol) ContentType(string) string { return "application/scim+json" }

func TestProtocolIntegersRetainTheirOwnRepresentation(t *testing.T) {
	api, router, f := setupWith(t, true, "application/scim+json")
	f.signedIn()
	f.allow = true
	httpx.Register(api.Surfaces(probe).App, huma.Operation{OperationID: "integer-protocol", Method: "GET", Path: "/integer-protocol"}, httpx.Permission("probe:read"),
		func(context.Context, *struct{}) (*struct{ Body integerProtocol }, error) {
			return &struct{ Body integerProtocol }{integerProtocol{9007199254740993}}, nil
		})
	got := integerRequest(router, "GET", at(api, "/integer-protocol"), "", "application/vnd.platformkit.int64+json", "")
	if got.Code != 200 || got.Header().Get("Content-Type") != "application/scim+json" {
		t.Fatalf("%d %s", got.Code, got.Body.String())
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(got.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if string(body["amount"]) != "9007199254740993" {
		t.Fatal(got.Body.String())
	}
	for _, op := range api.Recorded() {
		if op.OperationID == "integer-protocol" {
			for _, response := range op.Responses {
				if response.Content["application/vnd.platformkit.int64+json"] != nil {
					t.Fatal("protocol advertised decimal media")
				}
			}
		}
	}
}

func TestIntegerAPIsDoNotChangeTheEnginesSharedFormatTable(t *testing.T) {
	before := maps.Clone(huma.DefaultFormats)
	_, router, _ := exactHTTP(t)
	second, other, _ := setupWith(t, true)
	httpx.Register(second.Surfaces(probe).Public, huma.Operation{OperationID: "independent", Method: "GET", Path: "/independent"}, httpx.Public(), func(context.Context, *struct{}) (*struct{ Body struct{ Value int } }, error) { return nil, nil })
	after := huma.DefaultFormats
	if len(before) != len(after) || huma.DefaultFormats["application/vnd.platformkit.int64+json"].Marshal != nil {
		t.Fatal("shared format table changed")
	}
	for name, format := range before {
		if reflect.ValueOf(format.Marshal).Pointer() != reflect.ValueOf(after[name].Marshal).Pointer() || reflect.ValueOf(format.Unmarshal).Pointer() != reflect.ValueOf(after[name].Unmarshal).Pointer() {
			t.Fatal("shared codec changed")
		}
	}
	firstDoc := integerRequest(router, http.MethodGet, "/openapi.json?int64=string", "", "", "")
	secondDoc := integerRequest(other, http.MethodGet, "/openapi.json", "", "", "")
	if !strings.Contains(firstDoc.Body.String(), "ExactIntegerBodyInt64Input") || strings.Contains(secondDoc.Body.String(), "ExactIntegerBody") {
		t.Fatal("API schema leaked")
	}
}
