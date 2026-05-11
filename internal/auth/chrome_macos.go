//go:build darwin

package auth

import "context"

func ResolveChromeMacOS(ctx context.Context) (Cookies, error) {
	return ResolveChrome(ctx)
}
