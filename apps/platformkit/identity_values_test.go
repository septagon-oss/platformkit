package main

import (
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

func (c composition) testUsers() usercontracts.Service {
	return value[usercontracts.Service](c.planned)
}
func (c composition) testTenants() tenantcontracts.Service {
	return value[tenantcontracts.Service](c.planned)
}
func (c composition) testAuth() authcontracts.Auth { return value[authcontracts.Auth](c.planned) }
