package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/sameera/geronimo/internal/config"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadReadsSecretsFromEnvironment(t *testing.T) { // G24
	c, err := config.Load(env(map[string]string{
		"GERONIMO_BASE_URL":             "https://geronimo.example.ts.net/",
		"GERONIMO_GOOGLE_CLIENT_ID":     "id",
		"GERONIMO_GOOGLE_CLIENT_SECRET": "secret",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if c.GoogleClientID != "id" || c.GoogleClientSecret != "secret" {
		t.Fatalf("config = %+v", c)
	}
	if c.RedirectURL() != "https://geronimo.example.ts.net/auth/google/callback" {
		t.Fatalf("redirect = %q", c.RedirectURL())
	}
	if c.IdleLifetime != 7*24*time.Hour || c.AbsoluteLifetime != 30*24*time.Hour {
		t.Fatalf("lifetimes = %v %v", c.IdleLifetime, c.AbsoluteLifetime)
	}
}

func TestLoadRefusesMissingSettings(t *testing.T) {
	_, err := config.Load(env(map[string]string{}))
	if err == nil || !strings.Contains(err.Error(), "GERONIMO_GOOGLE_CLIENT_SECRET") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadRefusesBadBaseURL(t *testing.T) {
	_, err := config.Load(env(map[string]string{
		"GERONIMO_BASE_URL": "not a url", "GERONIMO_GOOGLE_CLIENT_ID": "a", "GERONIMO_GOOGLE_CLIENT_SECRET": "b",
	}))
	if err == nil {
		t.Fatal("bad URL accepted")
	}
}

func TestLoadRefusesNonPositiveLifetimes(t *testing.T) {
	for _, name := range []string{"GERONIMO_SESSION_IDLE", "GERONIMO_SESSION_ABSOLUTE"} {
		for _, v := range []string{"0", "-1h"} {
			_, err := config.Load(env(map[string]string{
				"GERONIMO_BASE_URL":             "https://geronimo.example.ts.net",
				"GERONIMO_GOOGLE_CLIENT_ID":     "id",
				"GERONIMO_GOOGLE_CLIENT_SECRET": "secret",
				name:                            v,
			}))
			if err == nil || !strings.Contains(err.Error(), name) {
				t.Fatalf("%s=%s: err = %v", name, v, err)
			}
		}
	}
}
