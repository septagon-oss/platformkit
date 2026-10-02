package main

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestTheDashboardCountsEveryKernelResourceInEachLanguage: the owner's "1 Settingss"
// — each card's name is the resource's declared form for its count, in the page's
// language: the singular at exactly one, the plural otherwise. The counts are the
// fixture's; only the forms are asserted.
func TestTheDashboardCountsEveryKernelResourceInEachLanguage(t *testing.T) {
	cfg, admin := languageApp(t, `{"default":"en","supported":["pt-PT"]}`)
	forms := map[string][][2]string{
		"en": {{"User", "Users"}, {"Setting", "Settings"}, {"Task", "Tasks"}, {"Plan", "Plans"},
			{"Content item", "Content items"}},
		"pt-PT": {{"Utilizador", "Utilizadores"}, {"Definição", "Definições"}, {"Tarefa", "Tarefas"},
			{"Plano", "Planos"}, {"Item de conteúdo", "Itens de conteúdo"}},
	}
	for ranking, names := range forms {
		html := dashboard(t, cfg, admin, ranking)
		if pageLang(html) != ranking {
			t.Fatalf("asked in %s, the dashboard declares lang=%q", ranking, pageLang(html))
		}
		for _, name := range names {
			card := regexp.MustCompile(`>(\d+) (` + regexp.QuoteMeta(name[0]) + `|` + regexp.QuoteMeta(name[1]) + `)<`).
				FindStringSubmatch(html)
			if card == nil {
				t.Errorf("the %s dashboard has no card named %s/%s", ranking, name[0], name[1])
				continue
			}
			n, _ := strconv.Atoi(card[1])
			if want := map[bool]string{true: name[0], false: name[1]}[n == 1]; card[2] != want {
				t.Errorf("the %s dashboard says %q %q, want %q", ranking, card[1], card[2], want)
			}
		}
		if strings.Contains(html, "Settingss") || strings.Contains(html, "Userss") {
			t.Errorf("the %s dashboard still doubles a plural", ranking)
		}
	}
}
