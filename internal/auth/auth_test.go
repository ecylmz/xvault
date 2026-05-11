package auth

import (
	"os"
	"path/filepath"
	"testing"
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
