// Package config reads Geronimo's settings from environment variables.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"
)

// Config holds the settings the service needs at startup.
type Config struct {
	Addr               string
	BaseURL            string
	DBPath             string
	GoogleClientID     string
	GoogleClientSecret string
	IdleLifetime       time.Duration
	AbsoluteLifetime   time.Duration
}

// Defaults for session lifetimes (decision record D3).
const (
	DefaultIdleLifetime     = 7 * 24 * time.Hour
	DefaultAbsoluteLifetime = 30 * 24 * time.Hour
)

// Load builds a Config from the given lookup function (os.Getenv in production).
func Load(getenv func(string) string) (Config, error) {
	c := Config{
		Addr:               valueOr(getenv("GERONIMO_ADDR"), "localhost:8080"),
		BaseURL:            strings.TrimRight(getenv("GERONIMO_BASE_URL"), "/"),
		DBPath:             valueOr(getenv("GERONIMO_DB_PATH"), "geronimo.db"),
		GoogleClientID:     getenv("GERONIMO_GOOGLE_CLIENT_ID"),
		GoogleClientSecret: getenv("GERONIMO_GOOGLE_CLIENT_SECRET"),
		IdleLifetime:       DefaultIdleLifetime,
		AbsoluteLifetime:   DefaultAbsoluteLifetime,
	}
	var missing []string
	for name, v := range map[string]string{
		"GERONIMO_BASE_URL":             c.BaseURL,
		"GERONIMO_GOOGLE_CLIENT_ID":     c.GoogleClientID,
		"GERONIMO_GOOGLE_CLIENT_SECRET": c.GoogleClientSecret,
	} {
		if v == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		slices.Sort(missing)
		return Config{}, fmt.Errorf("missing environment variables: %s", strings.Join(missing, ", "))
	}
	u, err := url.Parse(c.BaseURL)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return Config{}, errors.New("GERONIMO_BASE_URL must be an http(s) URL such as https://example.ts.net")
	}
	if v := getenv("GERONIMO_SESSION_IDLE"); v != "" {
		if c.IdleLifetime, err = time.ParseDuration(v); err != nil {
			return Config{}, fmt.Errorf("GERONIMO_SESSION_IDLE: %w", err)
		}
		if c.IdleLifetime <= 0 {
			return Config{}, errors.New("GERONIMO_SESSION_IDLE must be positive")
		}
	}
	if v := getenv("GERONIMO_SESSION_ABSOLUTE"); v != "" {
		if c.AbsoluteLifetime, err = time.ParseDuration(v); err != nil {
			return Config{}, fmt.Errorf("GERONIMO_SESSION_ABSOLUTE: %w", err)
		}
		if c.AbsoluteLifetime <= 0 {
			return Config{}, errors.New("GERONIMO_SESSION_ABSOLUTE must be positive")
		}
	}
	return c, nil
}

// FromEnv loads the Config from the process environment.
func FromEnv() (Config, error) { return Load(os.Getenv) }

// RedirectURL is the address Google returns the visitor to.
func (c Config) RedirectURL() string { return c.BaseURL + "/auth/google/callback" }

func valueOr(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
