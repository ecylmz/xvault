package config

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

type Config struct {
	Auth     AuthConfig     `toml:"auth" json:"auth"`
	Sync     SyncConfig     `toml:"sync" json:"sync"`
	Database DatabaseConfig `toml:"database" json:"database"`
	Export   ExportConfig   `toml:"export" json:"export"`
	Agent    AgentConfig    `toml:"agent" json:"agent"`
}

type AuthConfig struct {
	Sources    []string `toml:"sources" json:"sources"`
	DotenvPath string   `toml:"dotenv_path" json:"dotenv_path"`
	AuthToken  string   `toml:"auth_token" json:"-"`
	CT0        string   `toml:"ct0" json:"-"`
	TWID       string   `toml:"twid" json:"-"`
}

type SyncConfig struct {
	DefaultCount             int  `toml:"default_count" json:"default_count"`
	DefaultLikeCount         int  `toml:"default_like_count" json:"default_like_count"`
	DefaultBookmarkCount     int  `toml:"default_bookmark_count" json:"default_bookmark_count"`
	RequestDelayMS           int  `toml:"request_delay_ms" json:"request_delay_ms"`
	MaxRetries               int  `toml:"max_retries" json:"max_retries"`
	StopAfterRateLimits      int  `toml:"stop_after_consecutive_rate_limits" json:"stop_after_consecutive_rate_limits"`
	StoreRaw                 bool `toml:"store_raw" json:"store_raw"`
	CheckpointEveryItems     int  `toml:"checkpoint_every_items" json:"checkpoint_every_items"`
	CheckpointEveryPages     int  `toml:"checkpoint_every_pages" json:"checkpoint_every_pages"`
	DefaultThreadLimit       int  `toml:"default_thread_limit" json:"default_thread_limit"`
	DefaultConversationLimit int  `toml:"default_conversation_limit" json:"default_conversation_limit"`
	FeedDefaultHours         int  `toml:"feed_default_hours" json:"feed_default_hours"`
}

type DatabaseConfig struct {
	Path          string `toml:"path" json:"path"`
	WAL           bool   `toml:"wal" json:"wal"`
	BusyTimeoutMS int    `toml:"busy_timeout_ms" json:"busy_timeout_ms"`
	ForeignKeys   bool   `toml:"foreign_keys" json:"foreign_keys"`
}

type ExportConfig struct {
	Dir            string `toml:"dir" json:"dir"`
	MarkdownLayout string `toml:"markdown_layout" json:"markdown_layout"`
	Overwrite      bool   `toml:"overwrite" json:"overwrite"`
	HTMLWarnSizeMB int    `toml:"html_warn_size_mb" json:"html_warn_size_mb"`
}

type AgentConfig struct {
	SafeMode       bool `toml:"safe_mode" json:"safe_mode"`
	JSONDefault    bool `toml:"json_default" json:"json_default"`
	AllowDirectDB  bool `toml:"allow_direct_db" json:"allow_direct_db"`
	AllowRawOutput bool `toml:"allow_raw_output" json:"allow_raw_output"`
}

func Default() Config {
	return Config{
		Auth: AuthConfig{
			Sources:    []string{"env", "dotenv", "config", "firefox", "chrome", "macos_keychain"},
			DotenvPath: "~/.config/xvault/.env",
		},
		Sync: SyncConfig{
			DefaultCount: 100, DefaultLikeCount: -1, DefaultBookmarkCount: -1,
			RequestDelayMS: 750, MaxRetries: 5, StopAfterRateLimits: 3,
			StoreRaw: true, CheckpointEveryItems: 25, CheckpointEveryPages: 1,
			DefaultThreadLimit: 200, DefaultConversationLimit: 500, FeedDefaultHours: 24,
		},
		Database: DatabaseConfig{Path: "~/.local/share/xvault/xvault.sqlite", WAL: true, BusyTimeoutMS: 5000, ForeignKeys: true},
		Export:   ExportConfig{Dir: "~/.local/share/xvault/exports", MarkdownLayout: "collection/year", HTMLWarnSizeMB: 40},
		Agent:    AgentConfig{SafeMode: true},
	}
}

func Load(path string) (Config, string, error) {
	cfg := Default()
	if path == "" {
		path = "~/.config/xvault/config.toml"
	}
	resolved := Expand(path)
	b, err := os.ReadFile(resolved)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, resolved, nil
		}
		return cfg, resolved, err
	}
	if err := toml.Unmarshal(b, &cfg); err != nil {
		return cfg, resolved, err
	}
	return cfg, resolved, nil
}

func Expand(path string) string {
	if path == "" {
		return path
	}
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return os.ExpandEnv(path)
}

func EnsureDirs(cfg Config) error {
	for _, p := range []string{
		filepath.Dir(Expand("~/.config/xvault/config.toml")),
		filepath.Dir(Expand(cfg.Database.Path)),
		Expand(cfg.Export.Dir),
		Expand("~/.local/state/xvault/backups"),
		Expand("~/.local/state/xvault/logs"),
		Expand("~/.local/state/xvault/locks"),
	} {
		if err := os.MkdirAll(p, 0o700); err != nil {
			return err
		}
	}
	return nil
}
