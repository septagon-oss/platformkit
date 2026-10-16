package file_test

import (
	"encoding/json"
	"testing"
)

func TestEmptyLibraryKeepsAConsistentBodyTypeScale(t *testing.T) {
	router, _, _, _, _ := libraryFixture(t)
	for _, width := range []int{390, 1440} {
		result := libraryBrowser(t, router, libraryAddress, `
const vis=e=>{const r=e.getBoundingClientRect(),s=getComputedStyle(e);return r.width>40&&r.height>6&&s.visibility!=='hidden'&&s.display!=='none'};
const sizes=[...new Set([...document.querySelectorAll('p')].filter(vis).map(e=>Math.round(parseFloat(getComputedStyle(e).fontSize))))].sort((a,b)=>a-b);
document.body.dataset.result=JSON.stringify({width:innerWidth,sizes});`, width)
		var sizes []int
		if err := json.Unmarshal(result["sizes"], &sizes); err != nil {
			t.Fatal(err)
		}
		if len(sizes) > 2 {
			t.Errorf("empty library at %dpx uses %d body sizes %v; the design floor permits at most two", width, len(sizes), sizes)
		}
	}
}

func TestLibraryDetailsStayWithinTheDesignFloor(t *testing.T) {
	router, _, _, _, _ := libraryFixture(t)
	id, _ := uploadFrame(t, router, files)
	for _, width := range []int{390, 1440} {
		result := libraryBrowser(t, router, libraryAddress+"?file="+id, `
const vis=e=>{const r=e.getBoundingClientRect(),s=getComputedStyle(e);return r.width>40&&r.height>6&&s.visibility!=='hidden'&&s.display!=='none'};
const xs=[...document.querySelectorAll('h1,h2,h3,p')].filter(vis).map(e=>Math.round(e.getBoundingClientRect().left)).sort((a,b)=>a-b);
const edges=[];for(const x of xs){if(!edges.length||x-edges.at(-1)>3)edges.push(x)}
document.body.dataset.result=JSON.stringify({width:innerWidth,edges,overflow:document.documentElement.scrollWidth>innerWidth});`, width)
		var edges []int
		var overflow bool
		if err := json.Unmarshal(result["edges"], &edges); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(result["overflow"], &overflow); err != nil {
			t.Fatal(err)
		}
		if len(edges) > 2 || overflow {
			t.Errorf("library details at %dpx: edges=%v overflow=%v; want at most two alignments and no horizontal scrolling", width, edges, overflow)
		}
	}
}
