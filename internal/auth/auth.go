package auth

import (
	"bufio"
	"context"
	"errors"
	"os"
	"strings"

	"github.com/ecylmz/xvault/internal/config"
)

type Cookies struct {
	AuthToken string
	CT0       string
	TWID      string
}

type Source struct {
	Name string `json:"name"`
}

var ErrMissing = errors.New("authentication cookies missing")

func ShapeStatus(c Cookies) (bool, string) {
	if c.AuthToken == "" || c.CT0 == "" {
		return false, "auth_token or ct0 missing"
	}
	if len(c.AuthToken) < 8 || len(c.CT0) < 8 {
		return false, "auth_token or ct0 malformed"
	}
	if c.TWID != "" && (!strings.HasPrefix(c.TWID, "u=") || len(c.TWID) < 4) {
		return false, "twid malformed"
	}
	return true, "auth_token=present, ct0=present, twid=" + presence(c.TWID)
}

func RedactSecret(s string) string {
	if len(s) <= 8 {
		return "[REDACTED]"
	}
	return s[:4] + "..." + s[len(s)-4:]
}

func Resolve(ctx context.Context, cfg config.Config) (Cookies, Source, error) {
	for _, src := range cfg.Auth.Sources {
		switch src {
		case "env":
			if c := fromMap(getenv); c.AuthToken != "" && c.CT0 != "" {
				return c, Source{Name: "env"}, nil
			}
		case "dotenv":
			values, err := ParseDotenv(config.Expand(cfg.Auth.DotenvPath))
			if err == nil {
				if c := fromMap(func(k string) string { return values[k] }); c.AuthToken != "" && c.CT0 != "" {
					return c, Source{Name: "dotenv"}, nil
				}
			}
		case "config":
			c := Cookies{AuthToken: cfg.Auth.AuthToken, CT0: cfg.Auth.CT0, TWID: cfg.Auth.TWID}
			if c.AuthToken != "" && c.CT0 != "" {
				return c, Source{Name: "config"}, nil
			}
		case "firefox":
			if c, err := ResolveFirefox(ctx); err == nil && c.AuthToken != "" && c.CT0 != "" {
				return c, Source{Name: "firefox"}, nil
			}
		case "chrome":
			if c, err := ResolveChrome(ctx); err == nil && c.AuthToken != "" && c.CT0 != "" {
				return c, Source{Name: "chrome"}, nil
			}
		case "macos_keychain":
			if c, err := ResolveMacOSKeychain(ctx); err == nil && c.AuthToken != "" && c.CT0 != "" {
				return c, Source{Name: "macos_keychain"}, nil
			}
		}
	}
	return Cookies{}, Source{}, ErrMissing
}

func ResolveBrowser(ctx context.Context, source string) (Cookies, Source, error) {
	c, err := resolveBrowserCookies(ctx, source)
	if err != nil {
		return Cookies{}, Source{}, err
	}
	if c.AuthToken == "" || c.CT0 == "" {
		return Cookies{}, Source{}, ErrMissing
	}
	return c, Source{Name: source}, nil
}

func DotenvBody(c Cookies) string {
	body := "XVAULT_AUTH_TOKEN=\"" + c.AuthToken + "\"\nXVAULT_CT0=\"" + c.CT0 + "\"\n"
	if c.TWID != "" {
		body += "XVAULT_TWID=\"" + c.TWID + "\"\n"
	}
	return body
}

func Status(ctx context.Context, cfg config.Config) map[string]string {
	c, _, _ := Resolve(ctx, cfg)
	status := map[string]string{"auth_token": "missing", "ct0": "missing", "twid": "missing"}
	if c.AuthToken != "" {
		status["auth_token"] = "present"
	}
	if c.CT0 != "" {
		status["ct0"] = "present"
	}
	if c.TWID != "" {
		status["twid"] = "present"
	}
	return status
}

func fromMap(get func(string) string) Cookies {
	c := Cookies{
		AuthToken: first(get("XVAULT_AUTH_TOKEN"), get("TWITTER_AUTH_TOKEN")),
		CT0:       first(get("XVAULT_CT0"), get("TWITTER_CT0")),
		TWID:      first(get("XVAULT_TWID"), get("TWITTER_TWID")),
	}
	c.TWID = strings.Trim(c.TWID, "\"")
	return c
}

func getenv(k string) string { return os.Getenv(k) }

func first(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func presence(s string) string {
	if s == "" {
		return "missing"
	}
	return "present"
}

func ParseDotenv(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		parsed, err := parseDotenvValue(v)
		if err != nil {
			return nil, err
		}
		out[k] = parsed
	}
	return out, sc.Err()
}

func parseDotenvValue(v string) (string, error) {
	if strings.Contains(v, "\n") || strings.Contains(v, "\r") {
		return "", errors.New("invalid dotenv value: multiline values are not supported")
	}
	if strings.Contains(v, "$(") || strings.Contains(v, "`") {
		return "", errors.New("invalid dotenv value: command substitution is not supported")
	}
	if v == "" {
		return "", nil
	}
	if v[0] == '"' || v[0] == '\'' {
		if len(v) < 2 || v[len(v)-1] != v[0] {
			return "", errors.New("invalid dotenv value: unterminated quoted value")
		}
		return v[1 : len(v)-1], nil
	}
	if strings.Contains(v, "\"") || strings.Contains(v, "'") {
		return "", errors.New("invalid dotenv value: quotes must wrap the full value")
	}
	return v, nil
}

func EnvCookies() Cookies {
	return fromMap(getenv)
}
