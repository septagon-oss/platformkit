package admin

import (
	"golang.org/x/text/language"
	"golang.org/x/text/message/catalog"
)

// Messages supplies the sign-in page's English and European Portuguese copy.
// An application may add other modules' namespaced messages before passing the
// builder as a read-only catalog. Other admin screens remain English.
func Messages() *catalog.Builder {
	messages := catalog.NewBuilder(catalog.Fallback(language.English))
	for _, entry := range []struct{ key, english, portuguese string }{
		{"admin.login.title", "Sign in", "Iniciar sessão"},
		{"admin.login.description", "Use the address this tenant knows you by.", "Utilize o endereço associado à sua conta nesta organização."},
		{"admin.login.email", "Email", "Email"},
		{"admin.login.password", "Password", "Palavra-passe"},
	} {
		for tag, text := range map[language.Tag]string{language.English: entry.english, language.EuropeanPortuguese: entry.portuguese} {
			if err := messages.SetString(tag, entry.key, text); err != nil {
				panic(err)
			}
		}
	}
	return messages
}
