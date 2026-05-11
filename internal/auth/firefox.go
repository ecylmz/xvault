package auth

import "context"

func ResolveFirefox(ctx context.Context) (Cookies, error) {
	return resolveBrowserCookies(ctx, "firefox")
}
