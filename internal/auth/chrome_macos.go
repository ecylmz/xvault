//go:build darwin

package auth

import "context"

func ResolveChromeMacOS(ctx context.Context) (Cookies, error) {
	return resolveBrowserCookies(ctx, "chrome_macos")
}
