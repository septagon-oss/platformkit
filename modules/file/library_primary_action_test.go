package file_test

import (
	"encoding/json"
	"testing"
)

// The design floor allows one thing to do per view: at most one filled button of
// the page's primary colour above the fold, counted the way the design gate counts
// it. The library's empty page, its grid and an open panel each answer that at a
// phone's width and a desktop's.
func TestLibraryOffersOnePrimaryActionAboveTheFold(t *testing.T) {
	router, _, _, _, _ := libraryFixture(t)
	const count = `
const px=v=>Math.round(parseFloat(v)||0);
const parse=c=>{const m=c.match(/rgba?\(([^)]+)\)/);if(!m)return null;const p=m[1].split(',').map(Number);return {r:p[0],g:p[1],b:p[2],a:p.length>3?p[3]:1};};
const vis=e=>{const r=e.getBoundingClientRect();const s=getComputedStyle(e);return r.width>0&&r.height>0&&s.visibility!=='hidden'&&s.display!=='none';};
const ctas=[...document.querySelectorAll('a,button')].filter(vis).filter(e=>{const c=parse(getComputedStyle(e).backgroundColor);return c&&c.a>0&&(c.r+c.g+c.b)<600;}).filter(e=>e.getBoundingClientRect().top<innerHeight);
const by={};const words={};for(const e of ctas){const k=getComputedStyle(e).backgroundColor;by[k]=(by[k]||0)+1;(words[k]=words[k]||[]).push(e.textContent.trim());}
const top=Object.entries(by).sort((a,b)=>b[1]-a[1])[0];
document.body.dataset.result=JSON.stringify({primary:top?top[1]:0,words:top?words[top[0]]:[]});`
	measure := func(label, at string) {
		for _, width := range []int{390, 1440} {
			result := libraryBrowser(t, router, at, count, width)
			var primary int
			var words []string
			if err := json.Unmarshal(result["primary"], &primary); err != nil {
				t.Fatal(err)
			}
			_ = json.Unmarshal(result["words"], &words)
			if primary > 1 {
				t.Errorf("%s at %dpx: %d primary actions above the fold %q; the floor is one", label, width, primary, words)
			}
		}
	}
	measure("the empty library", libraryAddress)
	id, _ := uploadFrame(t, router, files)
	measure("the grid", libraryAddress)
	measure("the open panel", libraryAddress+"?file="+id)
}
