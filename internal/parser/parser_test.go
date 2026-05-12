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
	users := map[string]bool{}
	for _, u := range page.Users {
		users[u.Username] = true
	}
	if len(page.Users) != 2 || !users["alice"] || !users["bob"] {
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

func TestTimelineParsesRepostUsingOriginalTweetContent(t *testing.T) {
	raw := []byte(`[{"__typename":"Tweet","rest_id":"1234567890123456789","core":{"user_results":{"result":{"rest_id":"42000","legacy":{"screen_name":"retweeter","name":"Retweeter"}}}},"legacy":{"full_text":"RT @alice: Original &amp; archived","created_at":"Mon Jan 02 15:04:05 +0000 2026","user_id_str":"42000","conversation_id_str":"1234567890123456789","retweeted_status_result":{"result":{"__typename":"Tweet","rest_id":"9876543210987654321","core":{"user_results":{"result":{"rest_id":"43000","legacy":{"screen_name":"alice","name":"Alice"}}}},"legacy":{"full_text":"Original &amp; archived","lang":"en","reply_count":1,"retweet_count":2,"favorite_count":3,"quote_count":4,"entities":{"urls":[{"url":"https://t.co/o","expanded_url":"https://example.com/original","display_url":"example.com/original"}],"hashtags":[{"text":"Original"}]}}}}}}]`)
	page, err := Timeline(raw, "repost", "", "", "raw1")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Tweets) != 2 {
		t.Fatalf("tweets = %#v", page.Tweets)
	}
	var repost = page.Tweets[0]
	for _, tw := range page.Tweets {
		if tw.ID == "1234567890123456789" {
			repost = tw
			break
		}
	}
	if !repost.IsRetweet || repost.RetweetedTweetID != "9876543210987654321" {
		t.Fatalf("repost linkage = %#v", repost)
	}
	if repost.ID != "1234567890123456789" || repost.Text != "Original & archived" {
		t.Fatalf("repost content = %#v", repost)
	}
	if repost.AuthorID != "43000" || repost.AuthorUsername != "alice" || repost.AuthorDisplayName != "Alice" {
		t.Fatalf("repost author = %#v", repost)
	}
	if repost.ReplyCount != 1 || repost.RetweetCount != 2 || repost.LikeCount != 3 || repost.QuoteCount != 4 {
		t.Fatalf("repost metrics = %#v", repost)
	}
	var foundURL bool
	for _, u := range page.URLs {
		if u.TweetID == repost.ID && u.ExpandedURL == "https://example.com/original" {
			foundURL = true
			break
		}
	}
	if !foundURL {
		t.Fatalf("repost urls = %#v", page.URLs)
	}
	var foundTag bool
	for _, h := range page.Hashtags {
		if h.TweetID == repost.ID && h.Tag == "Original" {
			foundTag = true
			break
		}
	}
	if !foundTag {
		t.Fatalf("repost hashtags = %#v", page.Hashtags)
	}
}
