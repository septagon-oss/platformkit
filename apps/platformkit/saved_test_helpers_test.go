package main

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

func startSaved(t *testing.T, cfg config.Config, c composition) {
	t.Helper()
	opts := c.options
	opts.Role = app.All
	opts.Transport = memory.New()
	opts.Log = quiet()
	start(t, cfg, c.modules, opts)
}
