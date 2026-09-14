package events

import "github.com/septagon-oss/platformkit/kit/events/providers/memory"

// Memory retains the in-process transport constructor for existing outbox callers.
// Independent consumers can use providers/memory.New without the SQL outbox.
func Memory() Transport { return memory.New() }
