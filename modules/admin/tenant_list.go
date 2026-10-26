package admin

import tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"

func tenantList(deps Deps) tenantcontracts.Lister {
	if deps.TenantList != nil {
		return deps.TenantList
	}
	return deps.Tenants
}
