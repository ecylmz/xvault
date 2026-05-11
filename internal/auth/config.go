package auth

import "github.com/ecylmz/xvault/internal/config"

func FromConfig(cfg config.Config) Cookies {
	return Cookies{AuthToken: cfg.Auth.AuthToken, CT0: cfg.Auth.CT0, TWID: cfg.Auth.TWID}
}
