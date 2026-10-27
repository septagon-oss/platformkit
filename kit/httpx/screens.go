package httpx

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/danielgtaylor/huma/v2"
	"github.com/septagon-oss/platformkit/kit/screen"
)

const screenExtension = "x-platformkit-screen"

// DescribePage attaches typed screen metadata before the real route is mounted.
func (r *Router) DescribePage(op *huma.Operation, contract screen.ResponseSet) {
	if op.Extensions == nil {
		op.Extensions = map[string]any{}
	}
	contract.Responses = slices.Clone(contract.Responses)
	contract.Targets = slices.Clone(contract.Targets)
	op.Extensions[screenExtension] = contract
}

// WithViews declares generated views on this resource's router and returns a
// copy whose screen owner is that resource's module. No routes are discovered.
func (r *Router) WithViews(views ...screen.View) *Router {
	r.api.mu.Lock()
	defer r.api.mu.Unlock()
	for _, v := range views {
		if v.Ref().Owner() != cmp.Or(r.module, "platformkit") {
			panic(fmt.Errorf("httpx: view %s is not owned by %s", v.Ref(), r.module))
		}
		r.api.screenViews = append(r.api.screenViews, v)
	}
	copy := *r
	copy.screenOwner = r.module
	return &copy
}

// ScreenInventory validates the module declarations and all observed document
// routes. It reads no actor, service or resource closure.
func (a *API) ScreenInventory(app string, policy int, declared []screen.View) (screen.Inventory, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	views := append(slices.Clone(declared), a.screenViews...)
	mounts := []screen.Mount{}
	for _, m := range a.mounts {
		if m.operationID == "" {
			if m.page && !m.static {
				return screen.Inventory{}, fmt.Errorf("httpx: document route %s %s has no operation ID", m.method, m.path)
			}
			continue
		}
		owner := m.screenOwner
		if owner == "" {
			owner = "platformkit"
		}
		mounts = append(mounts, screen.Mount{
			Operation: screen.Operation{ID: m.operationID, Surface: string(m.surface), Method: m.method, Path: m.path, Guard: m.auth.String()},
			Owner:     owner, Document: m.page, Responses: slices.Clone(m.screens.Responses), Exception: m.screens.Exception, Reason: m.screens.Reason, Targets: slices.Clone(m.screens.Targets),
		})
	}
	return screen.Publish(app, policy, views, mounts)
}
