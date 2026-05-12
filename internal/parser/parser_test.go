package parser

import "testing"

func TestTimelineParsesTweetUserCursorAndCollection(t *testing.T) {
	raw := []byte(`[{"__typename":"Tweet","rest_id":"1234567890123456789","core":{"user_results":{"result":{"rest_id":"42000","legacy":{"screen_name":"alice","name":"Alice"}}}},"legacy":{"full_text":"Hello &amp; archive","created_at":"Mon Jan 02 15:04:05 +0000 2026","user_id_str":"42000","conversation_id_str":"1234567890123456789","favorite_count":5,"entities":{"urls":[{"url":"https://t.co/x","expanded_url":"https://example.com","display_url":"example.com"}],"user_mentions":[{"id_str":"43000","screen_name":"bob","name":"Bob"}],"hashtags":[{"text":"XVault"}]}}},{"entryType":"TimelineTimelineCursor","cursorType":"Bottom","value":"CURSOR"}]`)
	page, err := Timeline(raw, "bookmark", "", "", "raw1")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Tweets) != 1 || page.Tweets[0].Text != "Hello & archive" {
		t.Fatalf("unexpected tweets: %#v", page.Tweets)
	}
	if len(page.Users) != 2 || page.Users[0].Username != "alice" || page.Users[1].Username != "bob" {
		t.Fatalf("unexpected users: %#v", page.Users)
	}
	if len(page.Collections) != 1 || page.Collections[0].CollectionType != "bookmark" {
		t.Fatalf("unexpected collections: %#v", page.Collections)
	}
	if len(page.URLs) != 1 || page.URLs[0].ExpandedURL != "https://example.com" {
		t.Fatalf("unexpected urls: %#v", page.URLs)
	}
	if len(page.Mentions) != 1 || page.Mentions[0].Username != "bob" || page.Mentions[0].UserID != "43000" {
		t.Fatalf("unexpected mentions: %#v", page.Mentions)
	}
	if len(page.Hashtags) != 1 || page.Hashtags[0].Tag != "XVault" {
		t.Fatalf("unexpected hashtags: %#v", page.Hashtags)
	}
	if page.NextCursor != "CURSOR" {
		t.Fatalf("cursor = %q", page.NextCursor)
	}
}
