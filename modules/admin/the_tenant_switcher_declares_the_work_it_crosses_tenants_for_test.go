package admin_test

// declaredCrossTenantAccess is the cross-tenant need this module's manifest
// states, handed to the raw httpx.API the harnesses here build themselves. In an
// application kit/app carries that sentence from the manifest to the kernel, and
// a test that mounts the shell's Routes on an API of its own has to state the
// same fact: a token taken without a declaration is refused at composition.
// apps/platformkit's composition case is what checks the manifest still says this.
func declaredCrossTenantAccess() map[string]string {
	return map[string]string{"admin": "list every tenant for the workspace's tenant switcher"}
}
