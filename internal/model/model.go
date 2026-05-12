package model

import "time"

type User struct {
	ID          string `json:"id"`
	Username    string `json:"username,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	AvatarURL   string `json:"avatar_url,omitempty"`
	Verified    bool   `json:"verified"`
	Protected   bool   `json:"protected"`
}

type Tweet struct {
	ID                 string    `json:"id"`
	Text               string    `json:"text"`
	Lang               string    `json:"lang,omitempty"`
	AuthorID           string    `json:"author_id"`
	CreatedAt          string    `json:"created_at,omitempty"`
	ConversationID     string    `json:"conversation_id,omitempty"`
	InReplyToTweetID   string    `json:"in_reply_to_tweet_id,omitempty"`
	InReplyToUserID    string    `json:"in_reply_to_user_id,omitempty"`
	QuotedTweetID      string    `json:"quoted_tweet_id,omitempty"`
	RetweetedTweetID   string    `json:"retweeted_tweet_id,omitempty"`
	IsQuote            bool      `json:"is_quote"`
	IsRetweet          bool      `json:"is_retweet"`
	IsReply            bool      `json:"is_reply"`
	IsTombstone        bool      `json:"is_tombstone"`
	TombstoneReason    string    `json:"tombstone_reason,omitempty"`
	ReplyCount         int64     `json:"reply_count"`
	RetweetCount       int64     `json:"retweet_count"`
	LikeCount          int64     `json:"like_count"`
	QuoteCount         int64     `json:"quote_count"`
	BookmarkCount      *int64    `json:"bookmark_count,omitempty"`
	ViewCount          *int64    `json:"view_count,omitempty"`
	RawJSONID          string    `json:"raw_json_id,omitempty"`
	FirstSeenAt        time.Time `json:"first_seen_at"`
	LastSeenAt         time.Time `json:"last_seen_at"`
	AuthorUsername     string    `json:"author_username,omitempty"`
	AuthorDisplayName  string    `json:"author_display_name,omitempty"`
	BookmarkFolderID   string    `json:"bookmark_folder_id,omitempty"`
	BookmarkFolderName string    `json:"bookmark_folder_name,omitempty"`
}

type CollectionItem struct {
	TweetID            string `json:"tweet_id"`
	CollectionType     string `json:"collection_type"`
	BookmarkFolderID   string `json:"bookmark_folder_id,omitempty"`
	BookmarkFolderName string `json:"bookmark_folder_name,omitempty"`
	AddedAt            string `json:"added_at,omitempty"`
	SortIndex          string `json:"sort_index,omitempty"`
	SourceRunID        string `json:"source_run_id,omitempty"`
	ThreadID           string `json:"thread_id,omitempty"`
}

type URL struct {
	TweetID     string `json:"tweet_id"`
	URL         string `json:"url"`
	ExpandedURL string `json:"expanded_url,omitempty"`
	DisplayURL  string `json:"display_url,omitempty"`
}

type Media struct {
	ID          string `json:"id"`
	TweetID     string `json:"tweet_id"`
	MediaType   string `json:"media_type"`
	URL         string `json:"url,omitempty"`
	ExpandedURL string `json:"expanded_url,omitempty"`
	PreviewURL  string `json:"preview_url,omitempty"`
	AltText     string `json:"alt_text,omitempty"`
}

type Mention struct {
	TweetID     string `json:"tweet_id"`
	UserID      string `json:"user_id,omitempty"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name,omitempty"`
}

type Hashtag struct {
	TweetID string `json:"tweet_id"`
	Tag     string `json:"tag"`
}

type ParsedPage struct {
	Tweets      []Tweet
	Users       []User
	Collections []CollectionItem
	URLs        []URL
	Media       []Media
	Mentions    []Mention
	Hashtags    []Hashtag
	NextCursor  string
	RawID       string
}

type SearchResult struct {
	TweetID                 string   `json:"tweet_id"`
	URL                     string   `json:"url"`
	AuthorUsername          string   `json:"author_username"`
	AuthorDisplayName       string   `json:"author_display_name"`
	CreatedAt               string   `json:"created_at"`
	Collections             []string `json:"collections"`
	BookmarkFolderName      string   `json:"bookmark_folder_name,omitempty"`
	Score                   float64  `json:"score"`
	TextPreview             string   `json:"text_preview"`
	ReplyCount              int64    `json:"reply_count"`
	RepostCount             int64    `json:"repost_count"`
	LikeCount               int64    `json:"like_count"`
	QuoteCount              int64    `json:"quote_count"`
	HasMedia                bool     `json:"has_media"`
	HasLinks                bool     `json:"has_links"`
	LocalMarkdownPath       string   `json:"local_markdown_path,omitempty"`
	QuotedTweetID           string   `json:"quoted_tweet_id,omitempty"`
	QuotedTextPreview       string   `json:"quoted_text_preview,omitempty"`
	QuotedAuthorUsername    string   `json:"quoted_author_username,omitempty"`
	QuotedAuthorDisplayName string   `json:"quoted_author_display_name,omitempty"`
	ConversationID          string   `json:"conversation_id,omitempty"`
}
