package queryids

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"time"

	"github.com/ecylmz/xvault/internal/config"
)

type Entry struct {
	QueryID      string `json:"query_id"`
	Source       string `json:"source"`
	DiscoveredAt string `json:"discovered_at"`
}

type Cache struct {
	Version    int              `json:"version"`
	UpdatedAt  string           `json:"updated_at"`
	TTLHours   int              `json:"ttl_hours"`
	Operations map[string]Entry `json:"operations"`
}

var fallback = map[string]string{
	"Bookmarks":              "N2H8tZcGM5XLYU1L8KxM6A",
	"BookmarkSearchTimeline": "Nikib-QgCS0D_GU_YI8gBQ",
	"BookmarkFolderTimeline": "Lx3ZL8NwCnYMO2ii4yL7xQ",
	"Likes":                  "rB9KIiRz0_xi9KmNYS4vfA",
	"UserTweets":             "lrMzG9qPQHpqJdP3AbM-bQ",
	"UserTweetsAndReplies":   "3YJONShMAajim63A8iF-sw",
	"TweetDetail":            "_i0BBmP_dK_ZLFa2Y-ei9Q",
	"HomeLatestTimeline":     "PyccKQwFZpSmD1revPllaA",
	"Viewer":                 "_8ClT24oZ8tpylf_OSuNdg",
}

func Load(path string) Cache {
	c := Cache{Version: 1, TTLHours: 24, Operations: map[string]Entry{}}
	if path == "" {
		path = "~/.config/xvault/query-ids-cache.json"
	}
	b, err := os.ReadFile(config.Expand(path))
	if err == nil {
		_ = json.Unmarshal(b, &c)
	}
	for op, id := range fallback {
		if _, ok := c.Operations[op]; !ok {
			c.Operations[op] = Entry{QueryID: id, Source: "static", DiscoveredAt: time.Now().UTC().Format(time.RFC3339)}
		}
	}
	return c
}

func (c Cache) QueryID(op string) string {
	if e, ok := c.Operations[op]; ok {
		return e.QueryID
	}
	return fallback[op]
}

func ParseBundle(js string) map[string]string {
	out := map[string]string{}
	validID := regexp.MustCompile(`^[A-Za-z0-9_-]{8,128}$`)
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`operationName["']?\s*[:=]\s*["']([A-Za-z0-9_]+)["'][^{};\n]{0,400}?queryId["']?\s*[:=]\s*["']([A-Za-z0-9_-]{8,128})["']`),
		regexp.MustCompile(`queryId["']?\s*[:=]\s*["']([A-Za-z0-9_-]{8,128})["'][^{};\n]{0,400}?operationName["']?\s*[:=]\s*["']([A-Za-z0-9_]+)["']`),
		regexp.MustCompile(`["']([A-Za-z0-9_]+)["']\s*,\s*queryId\s*:\s*["']([A-Za-z0-9_-]{8,128})["']`),
	}
	for _, re := range patterns {
		for _, m := range re.FindAllStringSubmatch(js, -1) {
			op, id := m[1], m[2]
			if re == patterns[1] {
				id, op = m[1], m[2]
			}
			if validID.MatchString(id) {
				out[op] = id
			}
		}
	}
	return out
}

func Refresh(path string) (Cache, error) {
	if path == "" {
		path = "~/.config/xvault/query-ids-cache.json"
	}
	c := Load(path)
	client := &http.Client{Timeout: 30 * time.Second}
	pages := []string{"https://x.com/?lang=en", "https://x.com/explore", "https://x.com/notifications", "https://x.com/settings/profile"}
	return RefreshFromPages(context.Background(), path, c, pages, client)
}

func RefreshFromPages(ctx context.Context, path string, c Cache, pages []string, client *http.Client) (Cache, error) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	bundleRe := regexp.MustCompile(`https?://[^"'\s]+/responsive-web/client-web(?:-legacy)?/[A-Za-z0-9._-]+\.js`)
	found := map[string]string{}
	for _, page := range pages {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, page, nil)
		if err != nil {
			continue
		}
		req.Header.Set("user-agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124 Safari/537.36")
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		_ = resp.Body.Close()
		for _, bundle := range bundleRe.FindAllString(string(body), -1) {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, bundle, nil)
			if err != nil {
				continue
			}
			req.Header.Set("user-agent", "Mozilla/5.0")
			resp, err := client.Do(req)
			if err != nil {
				continue
			}
			js, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
			_ = resp.Body.Close()
			for op, id := range ParseBundle(string(js)) {
				found[op] = id
			}
		}
	}
	if len(found) == 0 {
		return c, fmt.Errorf("no query IDs discovered from X web bundles")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	c.UpdatedAt = now
	if c.Operations == nil {
		c.Operations = map[string]Entry{}
	}
	for op, id := range found {
		c.Operations[op] = Entry{QueryID: id, Source: "bundle", DiscoveredAt: now}
	}
	out, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return c, err
	}
	resolved := config.Expand(path)
	if err := os.MkdirAll(config.Expand("~/.config/xvault"), 0o700); err != nil {
		return c, err
	}
	if err := os.WriteFile(resolved, out, 0o600); err != nil {
		return c, err
	}
	return c, nil
}
