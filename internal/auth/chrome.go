package auth

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/sha1"
	"database/sql"
	"os"
	"path/filepath"
	"runtime"

	_ "modernc.org/sqlite"
)

func ResolveChrome(ctx context.Context) (Cookies, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Cookies{}, err
	}
	var patterns []string
	switch runtime.GOOS {
	case "darwin":
		patterns = []string{
			filepath.Join(home, "Library/Application Support/Google/Chrome/*/Cookies"),
			filepath.Join(home, "Library/Application Support/Google/Chrome/*/Network/Cookies"),
			filepath.Join(home, "Library/Application Support/Chromium/*/Cookies"),
			filepath.Join(home, "Library/Application Support/Chromium/*/Network/Cookies"),
		}
	default:
		patterns = []string{
			filepath.Join(home, ".config/google-chrome/*/Network/Cookies"),
			filepath.Join(home, ".config/google-chrome/*/Cookies"),
			filepath.Join(home, ".config/chromium/*/Network/Cookies"),
			filepath.Join(home, ".config/chromium/*/Cookies"),
		}
	}
	return ResolveChromeFromPatterns(ctx, patterns)
}

func ResolveChromeFromPatterns(ctx context.Context, patterns []string) (Cookies, error) {
	for _, pattern := range patterns {
		matches, _ := filepath.Glob(pattern)
		for _, path := range matches {
			c, err := readChromeCookies(ctx, path)
			if err == nil && c.AuthToken != "" && c.CT0 != "" {
				return c, nil
			}
		}
	}
	return Cookies{}, ErrMissing
}

func readChromeCookies(ctx context.Context, path string) (Cookies, error) {
	tmp, err := os.CreateTemp("", "xvault-chrome-cookies-*.sqlite")
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
	defer db.Close()
	rows, err := db.QueryContext(ctx, `SELECT name, value, encrypted_value FROM cookies WHERE (host_key LIKE '%.x.com' OR host_key = 'x.com' OR host_key LIKE '%.twitter.com' OR host_key = 'twitter.com') AND name IN ('auth_token','ct0','twid')`)
	if err != nil {
		return Cookies{}, err
	}
	defer rows.Close()
	values := map[string]string{}
	for rows.Next() {
		var name, value string
		var encrypted []byte
		if err := rows.Scan(&name, &value, &encrypted); err != nil {
			return Cookies{}, err
		}
		if value == "" {
			if decrypted, ok := decryptChromeCookieValue(ctx, encrypted); ok {
				value = decrypted
			}
		}
		if value == "" {
			continue
		}
		values[name] = value
	}
	if err := rows.Err(); err != nil {
		return Cookies{}, err
	}
	return Cookies{AuthToken: values["auth_token"], CT0: values["ct0"], TWID: values["twid"]}, nil
}

func decryptChromeCookieValueWithPassword(encrypted []byte, password string) (string, bool) {
	if len(encrypted) <= 3 || password == "" || !bytes.HasPrefix(encrypted, []byte("v10")) {
		return "", false
	}
	key, err := pbkdf2.Key(sha1.New, password, []byte("saltysalt"), 1003, 16)
	if err != nil {
		return "", false
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", false
	}
	ciphertext := encrypted[3:]
	if len(ciphertext) == 0 || len(ciphertext)%block.BlockSize() != 0 {
		return "", false
	}
	plain := make([]byte, len(ciphertext))
	iv := bytes.Repeat([]byte(" "), block.BlockSize())
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plain, ciphertext)
	padding := int(plain[len(plain)-1])
	if padding <= 0 || padding > block.BlockSize() || padding > len(plain) {
		return "", false
	}
	for _, b := range plain[len(plain)-padding:] {
		if int(b) != padding {
			return "", false
		}
	}
	return string(plain[:len(plain)-padding]), true
}
