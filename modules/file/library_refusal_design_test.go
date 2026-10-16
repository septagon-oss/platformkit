package file_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"testing"
)

func TestLibraryRefusalStaysWithinTheDesignFloor(t *testing.T) {
	router, _, _, _, _ := libraryFixture(t)
	id, _ := uploadFrame(t, router, files)
	panel := libraryRequest(t, router, http.MethodGet, libraryAddress+"?file="+id, host, "", true)
	stamp := regexp.MustCompile(`name="updated" value="([^"]+)"`).FindStringSubmatch(panel.Body.String())
	if panel.Code != http.StatusOK || len(stamp) != 2 {
		t.Fatal("the file's panel did not supply a revision for the caption")
	}
	refusal := libraryRequest(t, router, http.MethodPost, libraryAddress+"/"+id+"/alt", host,
		url.Values{"updated": {stamp[1]}, "alt": {""}}.Encode(), true)
	if refusal.Code != http.StatusUnprocessableEntity {
		t.Fatalf("empty caption = %d; want a validation refusal", refusal.Code)
	}
	markup := refusal.Body.String()
	rendered := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, markup)
	})
	for _, width := range []int{390, 1440} {
		result := libraryBrowser(t, rendered, libraryAddress, `
const vis=e=>{const r=e.getBoundingClientRect(),s=getComputedStyle(e);return r.width>40&&r.height>6&&s.visibility!=='hidden'&&s.display!=='none'};
const xs=[...document.querySelectorAll('h1,h2,h3,p')].filter(vis).map(e=>Math.round(e.getBoundingClientRect().left)).sort((a,b)=>a-b);
const edges=[];for(const x of xs){if(!edges.length||x-edges.at(-1)>3)edges.push(x)}
document.body.dataset.result=JSON.stringify({edges,overflow:document.documentElement.scrollWidth>innerWidth});`, width)
		var edges []int
		var overflow bool
		if err := json.Unmarshal(result["edges"], &edges); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(result["overflow"], &overflow); err != nil {
			t.Fatal(err)
		}
		if len(edges) > 2 || overflow {
			t.Errorf("caption refusal at %dpx: edges=%v overflow=%v; want at most two alignments and no horizontal scrolling", width, edges, overflow)
		}
	}
}
