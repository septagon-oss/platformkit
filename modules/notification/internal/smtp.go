package internal

import "github.com/septagon-oss/platformkit/kit/mail/providers/smtp"

// Mail is the standalone SMTP configuration retained for existing composition.
type Mail = smtp.Mail

// SMTP is the standalone sender retained for Notification integration.
type SMTP = smtp.SMTP

// NewSMTP keeps the existing constructor while sharing the provider.
func NewSMTP(cfg Mail) *SMTP { return smtp.New(cfg) }
