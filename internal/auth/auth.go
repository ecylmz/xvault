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

func RedactSecret(s string) string {
	if len(s) <= 8 {
		return "[REDACTED]"
	}
	return s[:4] + "..." + s[len(s)-4:]
}

func Resolve(ctx context.Context, cfg config.Config) (Cookies, Source, error) {
	_ = ctx
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
		}
	}
	return Cookies{}, Source{}, ErrMissing
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
		if strings.Contains(v, "\n") || strings.Contains(v, "\r") {
			continue
		}
		if len(v) >= 2 {
			if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
				v = v[1 : len(v)-1]
			}
		}
		out[k] = v
	}
	return out, sc.Err()
}
