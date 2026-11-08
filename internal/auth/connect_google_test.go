package auth_test

import (
	"context"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/sameera/geronimo/internal/auth"
	"github.com/sameera/geronimo/internal/gmail"
)

const verifier = "verifier-verifier-verifier-verifier-verifier-1234"

func (f *fakeIDP) connector() *auth.GoogleConnect {
	return auth.NewGoogleConnect(f.srv.URL, "client-id", "client-secret", "https://geronimo.example/connect/google/callback")
}

func TestConnectAuthURLAsksForGmailModifyOnly(t *testing.T) { // G1, D1
	f := newFakeIDP(t)
	raw, err := f.connector().AuthURL("st", "n1", verifier)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(raw)
	q := u.Query()
	want := []string{"openid", "email", gmail.ModifyScope}
	got := strings.Fields(q.Get("scope"))
	slices.Sort(want)
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("scope = %q", q.Get("scope"))
	}
	for k, v := range map[string]string{
		"state": "st", "nonce": "n1", "code_challenge_method": "S256", "access_type": "offline",
		"prompt": "consent select_account", "response_type": "code", "client_id": "client-id",
		"redirect_uri": "https://geronimo.example/connect/google/callback",
	} {
		if q.Get(k) != v {
			t.Errorf("%s = %q, want %q", k, q.Get(k), v)
		}
	}
	if q.Get("include_granted_scopes") != "" {
		t.Error("include_granted_scopes would widen the grant")
	}
	if q.Get("code_challenge") == "" || strings.Contains(raw, "verifier-verifier") {
		t.Errorf("PKCE challenge missing or verifier leaked: %s", raw)
	}
}

func TestSignInProviderStillAsksForIdentityOnly(t *testing.T) { // G4
	f := newFakeIDP(t)
	raw, _ := f.provider().AuthURL("st", "n1", verifier)
	if strings.Contains(raw, "gmail") || strings.Contains(raw, "offline") {
		t.Fatalf("sign-in asks for more than identity: %s", raw)
	}
}

func TestConnectExchange(t *testing.T) {
	ctx := context.Background()
	modify := "openid https://www.googleapis.com/userinfo/email " + gmail.ModifyScope
	t.Run("returns identity, tokens and granted scopes", func(t *testing.T) {
		f := newFakeIDP(t)
		f.scope = modify
		g, err := f.connector().Exchange(ctx, "code", verifier, "n1")
		if err != nil {
			t.Fatal(err)
		}
		if g.Subject != "sub-1" || g.Email != "a@example.com" || g.RefreshToken != "refresh-secret" || g.AccessToken != "access-secret" {
			t.Fatalf("grant = %+v", g)
		}
		if !slices.Contains(g.Scopes, gmail.ModifyScope) {
			t.Fatalf("scopes = %v", g.Scopes)
		}
		if f.tokenReq.Get("code_verifier") == "" {
			t.Error("code_verifier not sent")
		}
	})
	t.Run("a partial grant reports the scopes actually granted", func(t *testing.T) { // G10
		f := newFakeIDP(t)
		f.scope = "openid https://www.googleapis.com/auth/userinfo.email"
		g, err := f.connector().Exchange(ctx, "code", verifier, "n1")
		if err != nil {
			t.Fatal(err)
		}
		if slices.Contains(g.Scopes, gmail.ModifyScope) {
			t.Fatalf("scopes = %v", g.Scopes)
		}
	})
	t.Run("no refresh token is an error", func(t *testing.T) {
		f := newFakeIDP(t)
		f.scope = modify
		f.noRefresh = true
		if _, err := f.connector().Exchange(ctx, "code", verifier, "n1"); err == nil {
			t.Fatal("accepted a grant with no refresh token")
		}
	})
	bad := map[string]func(f *fakeIDP){
		"bad signature":  func(f *fakeIDP) { f.signWith = f.otherKey },
		"wrong issuer":   func(f *fakeIDP) { f.claims["iss"] = "https://evil.example" },
		"wrong audience": func(f *fakeIDP) { f.claims["aud"] = "other-client" },
		"expired":        func(f *fakeIDP) { f.claims["exp"] = 1 },
		"wrong nonce":    func(f *fakeIDP) { f.claims["nonce"] = "other" },
		"empty subject":  func(f *fakeIDP) { f.claims["sub"] = "" },
	}
	for name, mutate := range bad {
		t.Run(name, func(t *testing.T) {
			f := newFakeIDP(t)
			f.scope = modify
			mutate(f)
			if _, err := f.connector().Exchange(ctx, "code", verifier, "n1"); err == nil {
				t.Fatal("token accepted")
			}
		})
	}
	t.Run("errors never echo tokens", func(t *testing.T) { // G17
		f := newFakeIDP(t)
		f.scope = modify
		f.signWith = f.otherKey
		_, err := f.connector().Exchange(ctx, "code", verifier, "n1")
		if err == nil || strings.Contains(err.Error(), "access-secret") || strings.Contains(err.Error(), "refresh-secret") {
			t.Fatalf("err = %v", err)
		}
	})
}
