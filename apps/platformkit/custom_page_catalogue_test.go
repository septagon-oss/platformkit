package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/locale/providers/pseudo"
)

func TestCustomPagesUseTheCatalogueTheirApplicationServes(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	installed := catalogues()
	served := pseudo.Wrap(installed, pseudo.NewRecorder())
	c := composeCopy(cfg, installed, served)
	start(t, cfg, c.modules, appOptions(cfg, c, app.All))
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	selected := served.Select("pt-PT")
	for _, tc := range []struct {
		path, key, english, portuguese string
	}{
		{"/app/change/proposals", "change.queue.title", "Change reviews", "Revisões de alteração"},
		{"/app/change/proposals", "change.queue.empty.title", "Nothing is waiting", "Nada está à espera"},
		{"/app/change/proposals", "change.queue.empty.body", "No change in this tenant is open for a decision.", "Nenhuma alteração neste tenant está aberta a decisão."},
		{"/app/notification/notifications", "notification.inbox.title", "Notifications", "Notificações"},
		{"/app/notification/notifications", "notification.inbox.empty.title", "Nothing to read", "Nada para ler"},
		{"/app/notification/notifications", "notification.inbox.empty.body", "No notice has been addressed to you.", "Nenhuma notificação foi endereçada a si."},
		{"/app/file/files", "file.library.title", "Media library", "Biblioteca de ficheiros"},
	} {
		t.Run(tc.key, func(t *testing.T) {
			code, kind, body := get(t, cfg, admin, tc.path)
			if code != http.StatusOK || !strings.HasPrefix(kind, "text/html") {
				t.Fatalf("GET %s = %d %s", tc.path, code, kind)
			}
			if got := installed.Select("pt-PT").Text(tc.key, tc.english); got != tc.portuguese {
				t.Errorf("Portuguese copy = %q, want %q", got, tc.portuguese)
			}
			want := selected.Text(tc.key, tc.english)
			if !strings.Contains(string(body), want) {
				t.Errorf("%s does not render the served catalogue's %q", tc.path, want)
			}
		})
	}
}
