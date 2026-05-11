//go:build darwin

package auth

import (
	"context"
	"os/exec"
	"strings"
)

func ResolveChromeMacOS(ctx context.Context) (Cookies, error) {
	return ResolveChrome(ctx)
}

func decryptChromeCookieValue(ctx context.Context, encrypted []byte) (string, bool) {
	password, ok := chromeSafeStoragePassword(ctx)
	if !ok {
		return "", false
	}
	return decryptChromeCookieValueWithPassword(encrypted, password)
}

func chromeSafeStoragePassword(ctx context.Context) (string, bool) {
	for _, service := range []string{"Chrome Safe Storage", "Chromium Safe Storage"} {
		out, err := exec.CommandContext(ctx, "security", "find-generic-password", "-w", "-s", service).Output()
		if err != nil {
			continue
		}
		password := strings.TrimSpace(string(out))
		if password != "" {
			return password, true
		}
	}
	return "", false
}
