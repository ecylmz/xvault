package auth

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

func ResolveFirefox(ctx context.Context) (Cookies, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Cookies{}, err
	}
	patterns := []string{
		filepath.Join(home, ".mozilla/firefox/*/cookies.sqlite"),
		filepath.Join(home, "snap/firefox/common/.mozilla/firefox/*/cookies.sqlite"),
		filepath.Join(home, "Library/Application Support/Firefox/Profiles/*/cookies.sqlite"),
	}
	return ResolveFirefoxFromPatterns(ctx, patterns)
}

func ResolveFirefoxFromPatterns(ctx context.Context, patterns []string) (Cookies, error) {
	for _, pattern := range patterns {
		matches, _ := filepath.Glob(pattern)
		for _, path := range matches {
			c, err := readFirefoxCookies(ctx, path)
			if err == nil && c.AuthToken != "" && c.CT0 != "" {
				return c, nil
			}
		}
	}
	return Cookies{}, ErrMissing
}

func readFirefoxCookies(ctx context.Context, path string) (Cookies, error) {
	tmp, err := os.CreateTemp("", "xvault-firefox-cookies-*.sqlite")
	if err != nil {
		return Cookies{}, err
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	defer func() { _ = os.Remove(tmpPath) }()
	src, err := os.ReadFile(path)
	if err != nil {
		return Cookies{}, err
	}
	if err := os.WriteFile(tmpPath, src, 0o600); err != nil {
		return Cookies{}, err
	}
	db, err := sql.Open("sqlite", tmpPath+"?_pragma=query_only(1)")
	if err != nil {
		return Cookies{}, err
	}
	defer func() { _ = db.Close() }()
	rows, err := db.QueryContext(ctx, `SELECT name, value FROM moz_cookies WHERE (host LIKE '%.x.com' OR host = 'x.com' OR host LIKE '%.twitter.com' OR host = 'twitter.com') AND name IN ('auth_token','ct0','twid')`)
	if err != nil {
		return Cookies{}, err
	}
	defer func() { _ = rows.Close() }()
	values := map[string]string{}
	for rows.Next() {
		var name, value string
		if err := rows.Scan(&name, &value); err != nil {
			return Cookies{}, err
		}
		values[name] = value
	}
	if err := rows.Err(); err != nil {
		return Cookies{}, err
	}
	return Cookies{AuthToken: values["auth_token"], CT0: values["ct0"], TWID: values["twid"]}, nil
}
