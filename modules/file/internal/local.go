package internal

import "github.com/septagon-oss/platformkit/kit/blob/providers/local"

// Local is the standalone filesystem provider, retained for File integration.
type Local = local.Local

// NewLocal keeps the existing File constructor; no disk is touched until Put.
func NewLocal(dir string) *Local { return local.New(dir) }
