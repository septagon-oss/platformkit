package contracts

import "github.com/septagon-oss/platformkit/kit/richtext"

// Render forwards legacy content reads to the shared, sanitized Markdown renderer.
// New request paths use richtext.Render with their tenant-scoped Files port.
func Render(body string) (string, error) { return richtext.RenderLegacy(body) }
