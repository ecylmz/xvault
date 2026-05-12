//go:build !darwin

package auth

import "context"

func ResolveMacOSKeychain(ctx context.Context) (Cookies, error) {
	return Cookies{}, ErrMissing
}
