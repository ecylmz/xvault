package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/ecylmz/xvault/internal/auth"
)

const bearerToken = "AAAAAAAAAAAAAAAAAAAAANRILgAAAAAAnNwIzUejRCOuH5E6I8xnZz4puTs=1Zv7ttfk8LF81IUq16cHjhLTvJu4FA33AGWWjCpTnA"

type Operation struct {
	Name         string
	QueryID      string
	Method       string
	Variables    map[string]any
	Features     map[string]any
	FieldToggles map[string]any
}

type Options struct {
	BaseURL        string
	Auth           auth.Cookies
	Client         *http.Client
	MaxRetries     int
	RetryBaseDelay time.Duration
}

type Client struct {
	baseURL string
	auth    auth.Cookies
	http    *http.Client
	retries int
	backoff time.Duration
}

func New(opts Options) *Client {
	base := opts.BaseURL
	if base == "" {
		base = "https://x.com"
	}
	hc := opts.Client
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	retries := opts.MaxRetries
	if retries == 0 {
		retries = 2
	}
	backoff := opts.RetryBaseDelay
	if backoff == 0 {
		backoff = 750 * time.Millisecond
	}
	return &Client{baseURL: base, auth: opts.Auth, http: hc, retries: retries, backoff: backoff}
}

func BuildHeaders(c auth.Cookies) http.Header {
	h := http.Header{}
	h.Set("authorization", "Bearer "+bearerToken)
	h.Set("x-csrf-token", c.CT0)
	h.Set("x-twitter-active-user", "yes")
	h.Set("x-twitter-auth-type", "OAuth2Session")
	h.Set("x-twitter-client-language", "en")
	h.Set("accept", "application/json")
	h.Set("content-type", "application/json")
	cookie := "auth_token=" + c.AuthToken + "; ct0=" + c.CT0
	if c.TWID != "" {
		cookie += "; twid=" + c.TWID
	}
	h.Set("cookie", cookie)
	h.Set("user-agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124 Safari/537.36")
	return h
}

func DefaultFeatures() map[string]any {
	return map[string]any{
		"responsive_web_graphql_exclude_directive_enabled":                        true,
		"verified_phone_label_enabled":                                            false,
		"creator_subscriptions_tweet_preview_api_enabled":                         true,
		"responsive_web_graphql_timeline_navigation_enabled":                      true,
		"responsive_web_graphql_skip_user_profile_image_extensions_enabled":       false,
		"tweetypie_unmention_optimization_enabled":                                true,
		"responsive_web_edit_tweet_api_enabled":                                   true,
		"graphql_is_translatable_rweb_tweet_is_translatable_enabled":              true,
		"view_counts_everywhere_api_enabled":                                      true,
		"longform_notetweets_consumption_enabled":                                 true,
		"tweet_awards_web_tipping_enabled":                                        false,
		"freedom_of_speech_not_reach_fetch_enabled":                               true,
		"standardized_nudges_misinfo":                                             true,
		"tweet_with_visibility_results_prefer_gql_limited_actions_policy_enabled": true,
		"longform_notetweets_rich_text_read_enabled":                              true,
		"longform_notetweets_inline_media_enabled":                                true,
		"responsive_web_media_download_video_enabled":                             false,
		"responsive_web_enhance_cards_enabled":                                    false,
	}
}

func (c *Client) FetchGraphQL(ctx context.Context, op Operation) ([]byte, int, error) {
	if op.Features == nil {
		op.Features = DefaultFeatures()
	}
	endpoint := fmt.Sprintf("%s/i/api/graphql/%s/%s", c.baseURL, url.PathEscape(op.QueryID), url.PathEscape(op.Name))
	method := op.Method
	if method == "" {
		method = http.MethodGet
	}
	buildReq := func() (*http.Request, error) {
		if method == http.MethodPost {
			body, _ := json.Marshal(map[string]any{"variables": op.Variables, "features": op.Features, "fieldToggles": op.FieldToggles})
			return http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		}
		vars, _ := json.Marshal(op.Variables)
		features, _ := json.Marshal(op.Features)
		q := url.Values{}
		q.Set("variables", string(vars))
		q.Set("features", string(features))
		if op.FieldToggles != nil {
			fieldToggles, _ := json.Marshal(op.FieldToggles)
			q.Set("fieldToggles", string(fieldToggles))
		}
		return http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+q.Encode(), nil)
	}
	var lastBody []byte
	var lastStatus int
	var lastErr error
	for attempt := 0; attempt <= c.retries; attempt++ {
		req, err := buildReq()
		if err != nil {
			return nil, 0, err
		}
		req.Header = BuildHeaders(c.auth)
		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = err
			if attempt < c.retries {
				if err := sleepBackoff(ctx, c.backoff, attempt); err != nil {
					return nil, 0, err
				}
				continue
			}
			return nil, 0, err
		}
		b, readErr := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
		_ = resp.Body.Close()
		lastBody, lastStatus = b, resp.StatusCode
		if readErr != nil {
			return nil, resp.StatusCode, readErr
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return b, resp.StatusCode, nil
		}
		if !retryableStatus(resp.StatusCode) || attempt == c.retries {
			break
		}
		if err := sleepBackoff(ctx, c.backoff, attempt); err != nil {
			return nil, resp.StatusCode, err
		}
	}
	if lastErr != nil && lastStatus == 0 {
		return nil, 0, lastErr
	}
	return lastBody, lastStatus, fmt.Errorf("x graphql %s returned HTTP %d: %s", op.Name, lastStatus, sanitizeBody(lastBody))
}

func (c *Client) PostJSON(ctx context.Context, path string, payload any) ([]byte, int, error) {
	b, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(b))
	if err != nil {
		return nil, 0, err
	}
	req.Header = BuildHeaders(c.auth)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return out, resp.StatusCode, nil
}

func sanitizeBody(b []byte) string {
	if len(b) > 300 {
		b = b[:300]
	}
	return string(b)
}

func retryableStatus(status int) bool {
	return status == http.StatusBadGateway || status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout || status >= 500 && status < 600
}

func sleepBackoff(ctx context.Context, base time.Duration, attempt int) error {
	if base <= 0 {
		return nil
	}
	delay := base * time.Duration(1<<attempt)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(delay):
		return nil
	}
}
