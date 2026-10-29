package screens

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/entity/present"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/ui/page"
	"github.com/septagon-oss/platformkit/ui/resource"
)

func mountCollectionList(router *httpx.Router, shell page.Shell, o Options, res httpx.Resource, id string) {
	if !res.Offers(httpx.CRUDList) {
		return
	}
	page.Serve(router, shell, page.Route{View: screenRef(res, "list", ""), ID: id + "list", Method: http.MethodGet, Path: res.Path, Summary: "The " + res.Entity + " list",
		Subject: subject(res, o, res.Screen, false)}, res.ReadAuth(),
		func(ctx context.Context, req page.Request, in *listInput) (page.View, error) {
			o := localized(o, req)
			v := view(res, ctx, res.Screen, o)
			query, filters, err := listQuery(v, in)
			if err != nil {
				return page.View{}, rest.Fault(err)
			}
			rows, total, err := res.List(ctx, query)
			if err != nil {
				return page.View{}, err
			}
			v.Selected = filters
			return resource.List(v, o, rows, total, max(in.Page, 1), query.Sort, res.Writable(ctx)), nil
		})
}

func listQuery(r resource.Resource, in *listInput) (crud.Query, map[string]string, error) {
	policy := r.Resolved.List
	sort := in.Sort
	if sort == "" {
		sort = policy.DefaultSort
	}
	if sort != "" && sort != policy.DefaultSort && !slices.ContainsFunc(policy.Sorts, func(s present.ResolvedSortOption) bool { return s.Query == sort }) {
		return crud.Query{}, nil, fmt.Errorf("%w: unsupported sort %q", crud.ErrInvalid, sort)
	}
	query := crud.Query{Limit: perPage, Offset: (max(in.Page, 1) - 1) * perPage, Sort: sort, Filter: map[string]any{}}
	selected := map[string]string{}
	for _, raw := range in.Filter {
		name, value, ok := strings.Cut(raw, ":")
		if !ok {
			return crud.Query{}, nil, fmt.Errorf("%w: filter requires field:value", crud.ErrInvalid)
		}
		if _, duplicate := selected[name]; duplicate {
			return crud.Query{}, nil, fmt.Errorf("%w: duplicate filter %q", crud.ErrInvalid, name)
		}
		index := slices.IndexFunc(policy.Filters, func(f present.ResolvedFilter) bool { return f.Field == name })
		if index < 0 || !slices.ContainsFunc(policy.Filters[index].Values, func(v present.ResolvedOption) bool { return v.Value == value }) {
			return crud.Query{}, nil, fmt.Errorf("%w: unsupported filter %q", crud.ErrInvalid, raw)
		}
		field, ok := crud.FieldNamed(r.Schema.Fields, name)
		if !ok {
			return crud.Query{}, nil, fmt.Errorf("%w: unknown filter field %q", crud.ErrInvalid, name)
		}
		typed, err := crud.Coerce(field, value)
		if err != nil {
			return crud.Query{}, nil, err
		}
		query.Filter[name], selected[name] = typed, value
	}
	return query, selected, nil
}
