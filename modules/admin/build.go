package admin

import "github.com/septagon-oss/platformkit/modules/admin/internal"

// Build names the running reference — the brand and the version or revision the
// toolchain stamped — exactly as the workspace footer renders it. A command that
// has to say who is running before any page exists asks this rather than reading
// build info for itself, so a terminal and a page cannot name two different
// references.
func Build() string { return internal.Build() }
