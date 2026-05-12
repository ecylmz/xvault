package app

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ecylmz/xvault/internal/config"
	"github.com/ecylmz/xvault/internal/model"
	"github.com/ecylmz/xvault/internal/store"
)

func TestVersionJSON(t *testing.T) {
	code := Execute([]string{"version", "--json"})
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
}

func TestShowIncludeRawBlocked(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "x.sqlite")
	code, out := executeCaptureStdout(t, []string{"--db", db, "show", "123", "--include-raw", "--json"})
	if code == 0 {
		t.Fatal("expected RAW_OUTPUT_BLOCKED or not found failure")
	}
	if !strings.Contains(out, `"code":"RAW_OUTPUT_BLOCKED"`) {
		t.Fatalf("expected RAW_OUTPUT_BLOCKED output: %s", out)
	}
	if _, err := os.Stat(db); !os.IsNotExist(err) {
		t.Fatalf("raw block should happen before opening db, stat err=%v", err)
	}
}

func TestVerifyArchiveFailsForEmptyDB(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "empty.sqlite")
	code := Execute([]string{"--db", db, "verify-archive", "--json"})
	if code == 0 {
		t.Fatal("expected empty archive verification to fail")
	}
}

func TestVerifyArchiveSucceedsForQueryableBookmarksAndLikes(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "archive.sqlite")
	s, err := store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	err = s.UpsertPage(t.Context(), model.ParsedPage{
		Users: []model.User{{ID: "u1", Username: "author"}},
		Tweets: []model.Tweet{
			{ID: "1", Text: "the bookmarked tweet", AuthorID: "u1", CreatedAt: "2026-05-12T00:00:00Z"},
			{ID: "2", Text: "the liked tweet", AuthorID: "u1", CreatedAt: "2026-05-12T00:00:00Z"},
		},
		Collections: []model.CollectionItem{
			{TweetID: "1", CollectionType: "bookmark"},
			{TweetID: "2", CollectionType: "like"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	code := Execute([]string{"--db", db, "verify-archive", "--json"})
	if code != 0 {
		t.Fatalf("expected archive verification to pass, exit code = %d", code)
	}
}

func TestSyncThreadLimitUsesConversationDefault(t *testing.T) {
	cfg := config.Default()
	if got := syncThreadLimit(cfg, "conversation", 200, false); got != cfg.Sync.DefaultConversationLimit {
		t.Fatalf("conversation default limit = %d", got)
	}
	if got := syncThreadLimit(cfg, "conversation", 42, true); got != 42 {
		t.Fatalf("explicit conversation limit = %d", got)
	}
	if got := syncThreadLimit(cfg, "thread", 200, false); got != cfg.Sync.DefaultThreadLimit {
		t.Fatalf("thread default limit = %d", got)
	}
}

func TestDoctorDefaultDoesNotFailWhenChecksFail(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "doctor.sqlite")
	cfg := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfg, []byte("[auth]\nsources = [\"config\"]\ndotenv_path = \""+filepath.Join(dir, "missing.env")+"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code := Execute([]string{"--config", cfg, "--db", db, "doctor", "--json"})
	if code != 0 {
		t.Fatalf("expected default doctor to stay informational, exit code = %d", code)
	}
}

func TestDoctorStrictFailsWhenChecksFail(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "doctor.sqlite")
	cfg := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfg, []byte("[auth]\nsources = [\"config\"]\ndotenv_path = \""+filepath.Join(dir, "missing.env")+"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code := Execute([]string{"--config", cfg, "--db", db, "doctor", "--strict", "--json"})
	if code == 0 {
		t.Fatal("expected strict doctor to fail")
	}
}

func TestBackupVerifyRejectsInvalidDatabase(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "not-sqlite.sqlite")
	if err := os.WriteFile(path, []byte("not a sqlite database"), 0o600); err != nil {
		t.Fatal(err)
	}
	code := Execute([]string{"backup", "verify", path, "--json"})
	if code == 0 {
		t.Fatal("expected invalid backup verification to fail")
	}
}

func TestEmptyListJSONUsesArrays(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "empty.sqlite")
	code, out := executeCaptureStdout(t, []string{"--db", db, "search", "--recent", "--source", "bookmarks", "--json"})
	if code != 0 {
		t.Fatalf("search exit=%d output=%s", code, out)
	}
	if !strings.Contains(out, `"results":[]`) {
		t.Fatalf("empty search did not encode array: %s", out)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	code, out = executeCaptureStdout(t, []string{"backup", "list", "--json"})
	if code != 0 {
		t.Fatalf("backup list exit=%d output=%s", code, out)
	}
	if !strings.Contains(out, `"backups":[]`) {
		t.Fatalf("empty backups did not encode array: %s", out)
	}
}

func TestServiceExamplesIncludeBookmarksAndLikes(t *testing.T) {
	code, out := executeCaptureStdout(t, []string{"service", "cron", "print"})
	if code != 0 {
		t.Fatalf("exit code = %d output=%s", code, out)
	}
	for _, want := range []string{"sync bookmarks --count 300 --max-pages 5 --json", "sync likes --count 300 --max-pages 5 --json"} {
		if !strings.Contains(out, want) {
			t.Fatalf("cron output missing %q: %s", want, out)
		}
	}
	code, out = executeCaptureStdout(t, []string{"service", "systemd", "print", "--user"})
	if code != 0 {
		t.Fatalf("exit code = %d output=%s", code, out)
	}
	for _, want := range []string{"xvault-bookmarks.service", "xvault-likes.service", "sync bookmarks --count 300 --max-pages 5 --json", "sync likes --count 300 --max-pages 5 --json"} {
		if !strings.Contains(out, want) {
			t.Fatalf("systemd output missing %q: %s", want, out)
		}
	}
}

func TestSyncFeedHelpIncludesHoursFlag(t *testing.T) {
	code, out := executeCaptureStdout(t, []string{"sync", "feed", "--help"})
	if code != 0 {
		t.Fatalf("exit code = %d output=%s", code, out)
	}
	if !strings.Contains(out, "--hours") {
		t.Fatalf("sync feed help missing --hours: %s", out)
	}
}

func TestExportFailsWhenOperationLockExists(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	lockFile := filepath.Join(home, ".local/state/xvault/locks/xvault.lock")
	if err := os.MkdirAll(filepath.Dir(lockFile), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockFile, []byte(`{"pid":123,"started_at":"2026-01-01T00:00:00Z"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out := executeCaptureStdout(t, []string{"export", "json", "--json"})
	if code == 0 {
		t.Fatalf("expected locked export to fail: %s", out)
	}
	if !strings.Contains(out, `"code":"LOCKED"`) || !strings.Contains(out, `"retryable":true`) {
		t.Fatalf("expected retryable LOCKED output: %s", out)
	}
}

func executeCaptureStdout(t *testing.T, args []string) (int, string) {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	defer func() { os.Stdout = orig }()
	code := Execute(args)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	return code, string(out)
}

func TestErrorEnvelopeDoesNotLeakKnownSecretWords(t *testing.T) {
	var buf bytes.Buffer
	writeJSONError(&buf, "test", "AUTH_MISSING", "Authentication cookies appear to be expired.", false)
	var env Envelope
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.OK || env.Error == nil || env.Error.Code != "AUTH_MISSING" {
		t.Fatalf("bad envelope: %#v", env)
	}
	if bytes.Contains(buf.Bytes(), []byte("auth_token=")) {
		t.Fatalf("secret-like output leaked: %s", buf.String())
	}
	_ = os.Stdout
}

func TestAuthStatusReportsMalformedCookieShape(t *testing.T) {
	t.Setenv("XVAULT_AUTH_TOKEN", "a")
	t.Setenv("XVAULT_CT0", "c")
	t.Setenv("XVAULT_TWID", "1")
	code, out := executeCaptureStdout(t, []string{"--auth-source", "env", "auth", "status", "--json"})
	if code != 0 {
		t.Fatalf("auth status exit=%d output=%s", code, out)
	}
	var env Envelope
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatal(err)
	}
	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("auth status data = %#v", env.Data)
	}
	if data["valid_shape"] != false || data["shape_message"] != "auth_token or ct0 malformed" {
		t.Fatalf("auth status did not report malformed shape: %#v", data)
	}
	cookies, ok := data["cookies"].(map[string]any)
	if !ok {
		t.Fatalf("auth status cookies = %#v", data["cookies"])
	}
	for key, value := range cookies {
		if value != "present" && value != "missing" {
			t.Fatalf("cookie %s leaked value marker %#v", key, value)
		}
	}
}

func TestAuthTestRejectsMalformedCookieShape(t *testing.T) {
	t.Setenv("XVAULT_AUTH_TOKEN", "a")
	t.Setenv("XVAULT_CT0", "c")
	t.Setenv("XVAULT_TWID", "1")
	code, out := executeCaptureStdout(t, []string{"--auth-source", "env", "auth", "test", "--json"})
	if code != 4 {
		t.Fatalf("auth test exit=%d output=%s", code, out)
	}
	if !strings.Contains(out, `"code":"AUTH_MALFORMED"`) || !strings.Contains(out, "[REDACTED] or [REDACTED] malformed") {
		t.Fatalf("auth test did not report malformed auth: %s", out)
	}
}

func TestAuthImportEnvRejectsMalformedCookieShape(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XVAULT_AUTH_TOKEN", "a")
	t.Setenv("XVAULT_CT0", "c")
	t.Setenv("XVAULT_TWID", "1")
	cfgPath := filepath.Join(dir, "config.toml")
	dotenvPath := filepath.Join(dir, ".env")
	code, out := executeCaptureStdout(t, []string{"--config", cfgPath, "config", "set", "auth.dotenv_path", dotenvPath, "--json"})
	if code != 0 {
		t.Fatalf("config set exit=%d output=%s", code, out)
	}
	code, out = executeCaptureStdout(t, []string{"--config", cfgPath, "--auth-source", "env", "auth", "import-env", "--force", "--json"})
	if code != 4 {
		t.Fatalf("auth import-env exit=%d output=%s", code, out)
	}
	if !strings.Contains(out, `"code":"AUTH_MALFORMED"`) {
		t.Fatalf("auth import-env did not report malformed auth: %s", out)
	}
	if _, err := os.Stat(dotenvPath); !os.IsNotExist(err) {
		t.Fatalf("malformed import should not write dotenv, stat err=%v", err)
	}
}

func TestConfigSetErrorDoesNotEchoSecretValue(t *testing.T) {
	dir := t.TempDir()
	code, out := executeCaptureStdout(t, []string{"--config", filepath.Join(dir, "config.toml"), "config", "set", "auth.auth_token", "SECRET_VALUE", "--json"})
	if code == 0 {
		t.Fatal("expected unsupported secret config key to fail")
	}
	if strings.Contains(out, "SECRET_VALUE") {
		t.Fatalf("secret value leaked in output: %s", out)
	}
	if !strings.Contains(out, `"command":"config set"`) {
		t.Fatalf("unexpected command envelope: %s", out)
	}
}

func TestConfigGetSetNonSecretDefaults(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	code, out := executeCaptureStdout(t, []string{"--config", cfgPath, "config", "set", "sync.feed_default_hours", "12", "--json"})
	if code != 0 {
		t.Fatalf("config set exit=%d output=%s", code, out)
	}
	if !strings.Contains(out, `"value":12`) {
		t.Fatalf("config set output = %s", out)
	}
	code, out = executeCaptureStdout(t, []string{"--config", cfgPath, "config", "get", "sync.feed_default_hours", "--json"})
	if code != 0 {
		t.Fatalf("config get exit=%d output=%s", code, out)
	}
	if !strings.Contains(out, `"value":12`) {
		t.Fatalf("config get output = %s", out)
	}
	code, out = executeCaptureStdout(t, []string{"--config", cfgPath, "config", "set", "agent.json_default", "true", "--json"})
	if code != 0 {
		t.Fatalf("config bool set exit=%d output=%s", code, out)
	}
	code, out = executeCaptureStdout(t, []string{"--config", cfgPath, "config", "get", "agent.json_default", "--json"})
	if code != 0 || !strings.Contains(out, `"value":true`) {
		t.Fatalf("config bool get exit=%d output=%s", code, out)
	}
}

func TestSyncCountForCollectionUsesConfigDefaults(t *testing.T) {
	cfg := config.Default()
	count, all := syncCountForCollection(cfg, "likes", 100, false, false)
	if count != 0 || !all {
		t.Fatalf("likes defaults count=%d all=%v", count, all)
	}
	count, all = syncCountForCollection(cfg, "tweets", 100, false, false)
	if count != cfg.Sync.DefaultCount || all {
		t.Fatalf("tweets defaults count=%d all=%v", count, all)
	}
	count, all = syncCountForCollection(cfg, "likes", 25, false, true)
	if count != 25 || all {
		t.Fatalf("flag count=%d all=%v", count, all)
	}
	count, all = syncCountForCollection(cfg, "likes", 25, true, true)
	if count != 25 || !all {
		t.Fatalf("flag all count=%d all=%v", count, all)
	}
}

func TestInvokedCommandKeepsBooleanFlagsFromEatingCommands(t *testing.T) {
	got := invokedCommand([]string{"search", "--recent", "--source", "bookmarks", "--json"})
	if got != "search" {
		t.Fatalf("recent command = %q", got)
	}
	got = invokedCommand([]string{"auth", "import-browser", "--force", "--source", "chrome", "--json"})
	if got != "auth import-browser" {
		t.Fatalf("force command = %q", got)
	}
	got = invokedCommand([]string{"config", "set", "auth.auth_token", "SECRET_VALUE", "--json"})
	if got != "config set" {
		t.Fatalf("config set command = %q", got)
	}
	got = invokedCommand([]string{"search", "private query text", "--json"})
	if got != "search" {
		t.Fatalf("search command = %q", got)
	}
	got = invokedCommand([]string{"backup", "verify", "/private/path/archive.sqlite", "--json"})
	if got != "backup verify" {
		t.Fatalf("backup verify command = %q", got)
	}
	got = invokedCommand([]string{"service", "systemd", "print", "--user"})
	if got != "service systemd print" {
		t.Fatalf("service command = %q", got)
	}
}
