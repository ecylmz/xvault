//go:build linux

package auth

import "context"

func ResolveChromeLinux(ctx context.Context) (Cookies, error) {
	return ResolveChrome(ctx)
}
