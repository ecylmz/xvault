package auth

import "context"

type BrowserCookieSource struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	Status  string `json:"status"`
}

func BrowserSources() []BrowserCookieSource {
	return []BrowserCookieSource{
		{Name: "firefox", Enabled: true, Status: "best_effort"},
		{Name: "chrome_linux", Enabled: true, Status: "best_effort"},
		{Name: "chrome_macos", Enabled: true, Status: "best_effort_keychain_required"},
		{Name: "macos_keychain", Enabled: true, Status: "best_effort"},
	}
}

func resolveBrowserCookies(ctx context.Context, name string) (Cookies, error) {
	switch name {
	case "firefox":
		return ResolveFirefox(ctx)
	case "chrome", "chrome_linux", "chrome_macos":
		return ResolveChrome(ctx)
	case "macos_keychain":
		return ResolveMacOSKeychain(ctx)
	default:
		return Cookies{}, ErrMissing
	}
}
