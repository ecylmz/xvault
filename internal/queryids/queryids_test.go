package queryids

import "testing"

func TestParseBundleFormats(t *testing.T) {
	js := `
	  {operationName:"Bookmarks",queryId:"RV1g3b8n_SGOHwkqKYSCFw"};
	  {queryId:"ABCD1234_efgh",operationName:"Likes"};
	  ["UserTweets"],queryId:"USERtweets123";
	  {operationName:"Bad",queryId:"no"};
	`
	got := ParseBundle(js)
	if got["Bookmarks"] != "RV1g3b8n_SGOHwkqKYSCFw" {
		t.Fatalf("Bookmarks query id not parsed: %#v", got)
	}
	if got["Likes"] != "ABCD1234_efgh" {
		t.Fatalf("Likes query id not parsed: %#v", got)
	}
	if _, ok := got["Bad"]; ok {
		t.Fatalf("invalid query id should be ignored: %#v", got)
	}
}
