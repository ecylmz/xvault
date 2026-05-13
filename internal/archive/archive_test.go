package archive

import (
	archivezip "archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestParseZipImportsTweetsLikesBookmarksAndEntities(t *testing.T) {
	archivePath := writeArchive(t, map[string]string{
		"sample/data/account.js": `window.YTD.account.part0 = [
  { "account": { "accountId": "25401953", "username": "alice", "accountDisplayName": "Alice" } }
]`,
		"sample/data/tweets.js": `window.YTD.tweets.part0 = [
  { "tweet": {
    "id_str": "100000",
    "created_at": "Tue Jun 03 19:32:20 +0000 2025",
    "full_text": "@bob archive &amp; import https://t.co/local #xvault",
    "favorite_count": "12",
    "retweet_count": "3",
    "in_reply_to_status_id_str": "99999",
    "in_reply_to_user_id_str": "42",
    "in_reply_to_screen_name": "bob",
    "quoted_status_id_str": "100001",
    "entities": {
      "user_mentions": [{ "id_str": "42", "screen_name": "bob", "name": "Bob" }],
      "urls": [{ "url": "https://t.co/local", "expanded_url": "https://example.com/archive", "display_url": "example.com/archive" }],
      "hashtags": [{ "text": "xvault" }],
      "media": [{ "id_str": "m1", "media_url_https": "https://img.example.com/a.png", "type": "photo", "ext_alt_text": "Chart" }]
    },
    "extended_entities": {
      "media": [{ "id_str": "m1", "media_url_https": "https://img.example.com/a.png", "type": "photo", "ext_alt_text": "Chart" }]
    }
  } }
]`,
		"sample/data/note-tweet.js": `window.YTD.note_tweet.part0 = [
  { "noteTweet": { "noteTweetId": "100001", "createdAt": "2025-06-04T10:00:00.000Z", "core": { "text": "long archive note" } } }
]`,
		"sample/data/like.js": `window.YTD.like.part0 = [
  { "like": { "tweetId": "200000", "fullText": "liked item", "expandedUrl": "https://x.com/carol/status/200000", "likedAt": "2025-06-03T20:00:00.000Z" } }
]`,
		"sample/data/bookmark.js": `window.YTD.bookmark.part0 = [
  { "bookmark": { "tweetId": "300000", "fullText": "saved item", "expandedUrl": "https://twitter.com/dave/status/300000", "bookmarkedAt": "2025-06-03T21:00:00.000Z" } }
]`,
	})

	parsed, err := ParseZip(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Summary.AccountUsername != "alice" || parsed.Summary.TweetsSeen != 1 || parsed.Summary.NoteTweetsSeen != 1 || parsed.Summary.LikesSeen != 1 || parsed.Summary.BookmarksSeen != 1 {
		t.Fatalf("summary = %#v", parsed.Summary)
	}
	if len(parsed.Page.Tweets) != 4 {
		t.Fatalf("tweets = %#v", parsed.Page.Tweets)
	}
	tweet := parsed.Page.Tweets[0]
	if tweet.ID != "100000" || tweet.Text != "@bob archive & import https://t.co/local #xvault" || tweet.AuthorUsername != "alice" {
		t.Fatalf("tweet = %#v", tweet)
	}
	if tweet.CreatedAt != "2025-06-03T19:32:20Z" || !tweet.IsQuote || !tweet.IsReply || tweet.LikeCount != 12 || tweet.RetweetCount != 3 {
		t.Fatalf("tweet metadata = %#v", tweet)
	}
	if len(parsed.Page.Collections) != 4 {
		t.Fatalf("collections = %#v", parsed.Page.Collections)
	}
	collections := map[string]bool{}
	for _, c := range parsed.Page.Collections {
		collections[c.CollectionType+":"+c.TweetID] = true
	}
	for _, want := range []string{"tweet:100000", "tweet:100001", "like:200000", "bookmark:300000"} {
		if !collections[want] {
			t.Fatalf("missing collection %s in %#v", want, parsed.Page.Collections)
		}
	}
	if len(parsed.Page.URLs) != 1 || parsed.Page.URLs[0].ExpandedURL != "https://example.com/archive" {
		t.Fatalf("urls = %#v", parsed.Page.URLs)
	}
	if len(parsed.Page.Media) != 1 || parsed.Page.Media[0].AltText != "Chart" {
		t.Fatalf("media = %#v", parsed.Page.Media)
	}
	if len(parsed.Page.Mentions) != 1 || parsed.Page.Mentions[0].Username != "bob" {
		t.Fatalf("mentions = %#v", parsed.Page.Mentions)
	}
	if len(parsed.Page.Hashtags) != 1 || parsed.Page.Hashtags[0].Tag != "xvault" {
		t.Fatalf("hashtags = %#v", parsed.Page.Hashtags)
	}
}

func TestParseZipImportsDeletedTweetHeadersAsTombstones(t *testing.T) {
	archivePath := writeArchive(t, map[string]string{
		"data/account.js": `window.YTD.account.part0 = [{ "account": { "accountId": "1", "username": "alice" } }]`,
		"data/deleted-tweet-headers.js": `window.YTD.deleted_tweet_headers.part0 = [
  { "tweet": { "tweet_id": "400000", "user_id": "1", "created_at": "Tue Jun 03 19:32:20 +0000 2025" } }
]`,
	})
	parsed, err := ParseZip(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Summary.DeletedTweetsSeen != 1 || len(parsed.Page.Tweets) != 1 {
		t.Fatalf("parsed = %#v", parsed)
	}
	tweet := parsed.Page.Tweets[0]
	if tweet.ID != "400000" || !tweet.IsTombstone || tweet.TombstoneReason != "archive_deleted_tweet" {
		t.Fatalf("deleted tweet = %#v", tweet)
	}
}

func writeArchive(t *testing.T, files map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "twitter-archive.zip")
	out, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := archivezip.NewWriter(out)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	for _, name := range names {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(files[name])); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}
