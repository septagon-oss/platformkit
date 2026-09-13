package moduleplan_test

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/moduleplan"
)

func TestValidateRejectsIncompleteOrUnsafeConstructionOrders(t *testing.T) {
	for _, test := range []struct {
		name         string
		order        []string
		requirements map[string][]string
		want         string
	}{
		{"empty ordered name", []string{""}, map[string][]string{"": nil}, "empty module name"},
		{"empty defined name", nil, map[string][]string{"": nil}, "empty name"},
		{"unknown ordered name", []string{"ghost"}, nil, `"ghost", which is absent`},
		{"duplicate ordered name", []string{"user", "user"}, map[string][]string{"user": nil}, `"user" exactly once`},
		{"omitted definition", []string{"user"}, map[string][]string{"user": nil, "auth": {"user"}}, `omits module "auth"`},
		{"empty dependency", []string{"auth"}, map[string][]string{"auth": {""}}, `"auth" has an empty dependency`},
		{"unknown dependency", []string{"auth"}, map[string][]string{"auth": {"ghost"}}, `built from "ghost", which is absent`},
		{"duplicate dependency", []string{"user", "auth"}, map[string][]string{"user": nil, "auth": {"user", "user"}}, `dependency "user" twice`},
		{"later dependency", []string{"auth", "user"}, map[string][]string{"auth": {"user"}, "user": nil}, `built from "user", which must appear earlier`},
		{"self dependency", []string{"auth"}, map[string][]string{"auth": {"auth"}}, `built from "auth", which must appear earlier`},
		{"cycle", []string{"user", "auth"}, map[string][]string{"user": {"auth"}, "auth": {"user"}}, `built from "auth", which must appear earlier`},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := moduleplan.Validate(test.order, test.requirements); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Validate() = %v, want %q", err, test.want)
			}
		})
	}
}

func TestSelectRequiresExplicitDependenciesAndMandatoryModules(t *testing.T) {
	order := []string{"user", "notification", "auth", "content"}
	requirements := map[string][]string{"user": nil, "notification": {"user"}, "auth": {"notification"}, "content": nil}
	for _, test := range []struct {
		name               string
		mandatory, include []string
		want               string
	}{
		{"empty name", nil, []string{""}, "empty module name"},
		{"unknown selection", nil, []string{"ghost"}, `"ghost" is absent`},
		{"duplicate selection", nil, []string{"user", "user"}, `"user" appears twice`},
		{"missing mandatory", []string{"user"}, []string{"content"}, `does not include "user"`},
		{"empty selection missing mandatory", []string{"user"}, nil, `does not include "user"`},
		{"unknown mandatory", []string{"ghost"}, []string{"content"}, `does not include "ghost"`},
		{"missing direct dependency", nil, []string{"user", "auth"}, `built from "notification"`},
		{"missing transitive dependency", nil, []string{"notification", "auth"}, `built from "user"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := moduleplan.Select(order, requirements, test.mandatory, test.include)
			if got != nil || err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Select() = %v, %v; want no selection and %q", got, err, test.want)
			}
		})
	}
	if got, err := moduleplan.Select([]string{"ghost"}, requirements, nil, []string{"content"}); got != nil || err == nil {
		t.Fatalf("Select accepted an invalid definition: %v, %v", got, err)
	}
}

func TestSelectUsesConstructionOrderWithoutChangingOrRetainingInputs(t *testing.T) {
	order := []string{"user", "notification", "auth", "content"}
	requirements := map[string][]string{"user": nil, "notification": {"user"}, "auth": {"notification"}, "content": nil}
	mandatory, include := []string{"user"}, []string{"auth", "user", "notification"}
	selected, err := moduleplan.Select(order, requirements, mandatory, include)
	if err != nil || !slices.Equal(selected, []string{"user", "notification", "auth"}) {
		t.Fatalf("Select() = %v, %v", selected, err)
	}
	selected[0] = "changed"
	if !slices.Equal(order, []string{"user", "notification", "auth", "content"}) ||
		!slices.Equal(include, []string{"auth", "user", "notification"}) || !slices.Equal(mandatory, []string{"user"}) ||
		!reflect.DeepEqual(requirements, map[string][]string{"user": nil, "notification": {"user"}, "auth": {"notification"}, "content": nil}) {
		t.Fatal("selection modified or retained caller-owned input storage")
	}
	again, err := moduleplan.Select(order, requirements, mandatory, include)
	if err != nil || !slices.Equal(again, []string{"user", "notification", "auth"}) {
		t.Fatalf("a previous result changed later selection: %v, %v", again, err)
	}
	for _, include := range [][]string{nil, {"content"}} {
		got, err := moduleplan.Select(order, requirements, nil, include)
		if err != nil || !slices.Equal(got, include) {
			t.Fatalf("Select enabled unrequested modules: %v, %v", got, err)
		}
	}
	if got, err := moduleplan.Select(nil, nil, nil, nil); err != nil || len(got) != 0 {
		t.Fatalf("empty definition and selection: %v, %v", got, err)
	}
}

func TestValidateReportsOmittedDefinitionsDeterministically(t *testing.T) {
	for range 30 {
		err := moduleplan.Validate(nil, map[string][]string{"zebra": nil, "alpha": nil})
		if err == nil || err.Error() != `construction order omits module "alpha"` {
			t.Fatalf("Validate() = %v, want the first omitted name alphabetically", err)
		}
	}
}
