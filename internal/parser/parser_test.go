package parser

import "testing"

func TestTimelineParsesTweetUserCursorAndCollection(t *testing.T) {
	raw := []byte(`[{"__typename":"Tweet","rest_id":"1234567890123456789","core":{"user_results":{"result":{"rest_id":"42000","legacy":{"screen_name":"alice","name":"Alice"}}}},"legacy":{"full_text":"Hello &amp; archive","created_at":"Mon Jan 02 15:04:05 +0000 2026","user_id_str":"42000","conversation_id_str":"1234567890123456789","favorite_count":5,"entities":{"urls":[{"url":"https://t.co/x","expanded_url":"https://example.com","display_url":"example.com"}]}}},{"entryType":"TimelineTimelineCursor","cursorType":"Bottom","value":"CURSOR"}]`)
	page, err := Timeline(raw, "bookmark", "", "", "raw1")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Tweets) != 1 || page.Tweets[0].Text != "Hello & archive" {
		t.Fatalf("unexpected tweets: %#v", page.Tweets)
	}
	if len(page.Users) != 1 || page.Users[0].Username != "alice" {
		t.Fatalf("unexpected users: %#v", page.Users)
	}
	if len(page.Collections) != 1 || page.Collections[0].CollectionType != "bookmark" {
		t.Fatalf("unexpected collections: %#v", page.Collections)
	}
	if len(page.URLs) != 1 || page.URLs[0].ExpandedURL != "https://example.com" {
		t.Fatalf("unexpected urls: %#v", page.URLs)
	}
	if page.NextCursor != "CURSOR" {
		t.Fatalf("cursor = %q", page.NextCursor)
	}
}
