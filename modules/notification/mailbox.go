package notification

import "github.com/septagon-oss/platformkit/kit/mail/providers/memory"

// Mailbox is the standalone in-memory provider. Bodies stay in memory and
// never enter its logs; it sends no messages to an external mail server.
type Mailbox = memory.Mailbox

// NewMailbox keeps the existing constructor for application composition.
func NewMailbox() *Mailbox { return memory.New() }
