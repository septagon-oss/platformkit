package file_test

import (
	"encoding/json"
	"testing"
)

func TestDecorativeImageCanBeSavedWithoutAltText(t *testing.T) {
	router, _, _, _, _ := libraryFixture(t)
	id, _ := uploadFrame(t, router, files)
	result := libraryBrowser(t, router, libraryAddress+"?file="+id, `
const form=document.querySelector('form[action$="/alt"]');
const decorative=form.querySelector('[name="decorative"]');
decorative.checked=true;decorative.dispatchEvent(new Event('change',{bubbles:true}));
form.querySelector('[name="alt"]').value='';
document.body.dataset.result=JSON.stringify({valid:form.checkValidity(),missing:form.querySelector('[name="alt"]').validity.valueMissing});`, 390)
	var valid, missing bool
	if err := json.Unmarshal(result["valid"], &valid); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(result["missing"], &missing); err != nil {
		t.Fatal(err)
	}
	if !valid || missing {
		t.Errorf("a checked Decorative image with empty alt cannot submit: valid=%v valueMissing=%v", valid, missing)
	}
}
