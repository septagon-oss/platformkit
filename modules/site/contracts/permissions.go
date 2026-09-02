package contracts

// The one permission a site has. There is no site:read: reading the settings is
// reading how the site is configured, which is the same act as configuring it,
// and the half a visitor needs is the public route — which asks for nothing
// because publishing a site is deciding that anybody may see it.
//
// The list the manifest declares is in ../module.go, which keeps kit/module out
// of this package's build graph.
const PermissionSiteManage = "site:manage"
