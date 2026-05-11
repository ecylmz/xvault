//go:build !darwin

package auth

import "context"

func decryptChromeCookieValue(ctx context.Context, encrypted []byte) (string, bool) {
	return "", false
}
