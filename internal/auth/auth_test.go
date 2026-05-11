package auth

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestParseDotenvQuotedAndAliases(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("TWITTER_AUTH_TOKEN=\"secret-token\"\nTWITTER_CT0='csrf-token'\nTWITTER_TWID=u=123\n# ignored\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ParseDotenv(path)
	if err != nil {
		t.Fatal(err)
	}
	c := fromMap(func(k string) string { return got[k] })
	if c.AuthToken != "secret-token" || c.CT0 != "csrf-token" || c.TWID != "u=123" {
		t.Fatalf("unexpected cookies: %#v", c)
	}
}

func TestRedactSecret(t *testing.T) {
	if got := RedactSecret("12345678"); got != "[REDACTED]" {
		t.Fatalf("short secret redaction = %q", got)
	}
	if got := RedactSecret("1234567890abcdef"); got != "1234...cdef" {
		t.Fatalf("long secret redaction = %q", got)
	}
}

func TestResolveFirefoxFromPatterns(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "profile.default")
	if err := os.MkdirAll(profile, 0o700); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(profile, "cookies.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE moz_cookies(host TEXT, name TEXT, value TEXT);
INSERT INTO moz_cookies(host,name,value) VALUES('.x.com','auth_token','auth'),('.x.com','ct0','csrf'),('.x.com','twid','u=1')`); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	c, err := ResolveFirefoxFromPatterns(context.Background(), []string{filepath.Join(dir, "*", "cookies.sqlite")})
	if err != nil {
		t.Fatal(err)
	}
	if c.AuthToken != "auth" || c.CT0 != "csrf" || c.TWID != "u=1" {
		t.Fatalf("cookies = %#v", c)
	}
}
