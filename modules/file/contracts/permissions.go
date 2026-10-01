package contracts

// The four permissions files have. Reading is reading a private file's bytes,
// minting a grant for them, and listing what there is; managing is uploading and
// deleting. A public file's bytes need neither, which is what "public" means.
//
// Erasing is a different promise from deleting: "and nothing is left" is a
// stronger sentence than "the row is gone", so it has a permission of its own
// rather than borrowing file:manage. Retaining is the same in the other
// direction — placing a hold stops the clock for everybody else's deletion — so
// it is not something a reader does by default. Both are declared in the
// manifest, which is what lets kit/app refuse a route that guards itself with a
// permission nobody can be granted.
//
// The list the manifest declares is in ../module.go, which keeps kit/module out
// of this package's build graph.
const (
	PermissionFileRead   = "file:read"
	PermissionFileManage = "file:manage"
	PermissionFileErase  = "file:erase"
	PermissionFileRetain = "file:retain"
)
