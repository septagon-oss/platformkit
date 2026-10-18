package notification_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
)

func TestSenderPermissionHasWordsInEachSupportedLanguage(t *testing.T) {
	_, manifest := notification.New(notification.Deps{Mailer: notification.NewMailbox()})
	if err := module.Validate([]module.Module{manifest}); err != nil {
		t.Fatalf("notification cannot compose: %v", err)
	}
	if len(manifest.Permissions) != 1 || manifest.Permissions[0].Key != contracts.PermissionSenderManage {
		t.Fatalf("sender grant declaration: %+v", manifest.Permissions)
	}
	grant := manifest.Permissions[0]
	words := xtext.Load("en", notification.Catalogue())
	for _, tc := range []struct {
		preference string
		language   string
		label      string
	}{
		{"pt-PT,pt;q=0.9,en;q=0.8", "pt-PT", "gerir o endereço de envio do tenant"},
		{"en", "en", "manage the tenant's sending address"},
		{"pt-PT", "pt-PT", "gerir o endereço de envio do tenant"},
	} {
		t.Run(tc.preference, func(t *testing.T) {
			loc := words.Select(tc.preference)
			if loc.Language != tc.language {
				t.Errorf("selected language = %q, want %q", loc.Language, tc.language)
			}
			if got := loc.Text("permission."+grant.Key, grant.Label); got != tc.label {
				t.Errorf("sender permission label = %q, want %q", got, tc.label)
			}
		})
	}
}
