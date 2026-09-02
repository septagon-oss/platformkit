package contracts

// The two permissions files have. Reading is reading a private file's bytes and
// listing what there is; managing is uploading and deleting. A public file's
// bytes need neither, which is what "public" means.
//
// The list the manifest declares is in ../module.go, which keeps kit/module out
// of this package's build graph.
const (
	PermissionFileRead   = "file:read"
	PermissionFileManage = "file:manage"
)
