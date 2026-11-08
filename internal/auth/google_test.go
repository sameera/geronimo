package auth_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"

	"github.com/sameera/geronimo/internal/auth"
)

// fakeIDP is a minimal OpenID Connect provider.
type fakeIDP struct {
	srv      *httptest.Server
	key      *rsa.PrivateKey
	otherKey *rsa.PrivateKey

	mu            sync.Mutex
	claims        map[string]any // overrides merged over the valid claims
	signWith      *rsa.PrivateKey
	tokenReq      url.Values
	scope         string // granted scopes reported with the token; empty omits the field
	noRefresh     bool
	refreshStatus int // when set, refresh-token requests answer with this status
	refreshBody   string
	refreshScope  string // scope reported on a successful refresh
}

func newFakeIDP(t *testing.T) *fakeIDP {
	t.Helper()
	k1, _ := rsa.GenerateKey(rand.Reader, 2048)
	k2, _ := rsa.GenerateKey(rand.Reader, 2048)
	f := &fakeIDP{key: k1, otherKey: k2, signWith: k1, claims: map[string]any{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                f.srv.URL,
			"authorization_endpoint":                f.srv.URL + "/auth",
			"token_endpoint":                        f.srv.URL + "/token",
			"jwks_uri":                              f.srv.URL + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{
			{Key: &f.key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"},
		}})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.mu.Lock()
		defer f.mu.Unlock()
		f.tokenReq = r.PostForm
		if r.PostForm.Get("grant_type") == "refresh_token" {
			w.Header().Set("Content-Type", "application/json")
			if f.refreshStatus != 0 {
				w.WriteHeader(f.refreshStatus)
				_, _ = w.Write([]byte(f.refreshBody))
				return
			}
			resp := map[string]any{"access_token": "fresh-access", "token_type": "Bearer", "expires_in": 3600}
			if f.refreshScope != "" {
				resp["scope"] = f.refreshScope
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		c := map[string]any{
			"iss": f.srv.URL, "aud": "client-id", "sub": "sub-1", "nonce": "n1",
			"email": "a@example.com", "name": "A",
			"exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(),
		}
		for k, v := range f.claims {
			c[k] = v
		}
		signer, _ := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256,
			Key: jose.JSONWebKey{Key: f.signWith, KeyID: "k1"}}, (&jose.SignerOptions{}).WithType("JWT"))
		payload, _ := json.Marshal(c)
		obj, _ := signer.Sign(payload)
		raw, _ := obj.CompactSerialize()
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"access_token": "access-secret", "refresh_token": "refresh-secret",
			"token_type": "Bearer", "expires_in": 3600, "id_token": raw,
		}
		if f.scope != "" {
			resp["scope"] = f.scope
		}
		if f.noRefresh {
			delete(resp, "refresh_token")
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeIDP) provider() *auth.Google {
	return auth.NewGoogle(f.srv.URL, "client-id", "client-secret", "https://geronimo.example/auth/google/callback")
}

func TestGoogleAuthURLAsksForIdentityOnly(t *testing.T) { // G1
	f := newFakeIDP(t)
	raw, err := f.provider().AuthURL("st", "n1", "verifier-verifier-verifier-verifier-verifier-1234")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(raw)
	q := u.Query()
	if got := q.Get("scope"); got != "openid email profile" {
		t.Errorf("scope = %q", got)
	}
	if q.Get("access_type") != "" || strings.Contains(raw, "offline") {
		t.Errorf("offline access requested: %s", raw)
	}
	if strings.Contains(raw, "gmail") || strings.Contains(raw, "mail.google.com") {
		t.Errorf("mail scope requested: %s", raw)
	}
	for k, want := range map[string]string{
		"state": "st", "nonce": "n1", "code_challenge_method": "S256",
		"prompt": "select_account", "response_type": "code", "client_id": "client-id",
	} {
		if q.Get(k) != want {
			t.Errorf("%s = %q, want %q", k, q.Get(k), want)
		}
	}
	if q.Get("code_challenge") == "" || strings.Contains(raw, "verifier-verifier") {
		t.Errorf("PKCE challenge missing or verifier leaked: %s", raw)
	}
}

func TestGoogleExchange(t *testing.T) {
	ctx := context.Background()
	t.Run("valid token yields only subject, email and name; sends PKCE verifier", func(t *testing.T) { // G3
		f := newFakeIDP(t)
		c, err := f.provider().Exchange(ctx, "code", "verifier-verifier-verifier-verifier-verifier-1234", "n1")
		if err != nil {
			t.Fatal(err)
		}
		if c.Subject != "sub-1" || c.Email != "a@example.com" || c.Name != "A" {
			t.Fatalf("claims = %+v", c)
		}
		if f.tokenReq.Get("code_verifier") == "" {
			t.Error("code_verifier not sent")
		}
	})

	bad := map[string]func(f *fakeIDP){
		"bad signature":  func(f *fakeIDP) { f.signWith = f.otherKey },
		"wrong issuer":   func(f *fakeIDP) { f.claims["iss"] = "https://evil.example" },
		"wrong audience": func(f *fakeIDP) { f.claims["aud"] = "other-client" },
		"expired":        func(f *fakeIDP) { f.claims["exp"] = time.Now().Add(-time.Hour).Unix() },
		"wrong nonce":    func(f *fakeIDP) { f.claims["nonce"] = "other" },
		"missing nonce":  func(f *fakeIDP) { f.claims["nonce"] = "" },
	}
	for name, mutate := range bad { // G12
		t.Run(name, func(t *testing.T) {
			f := newFakeIDP(t)
			mutate(f)
			if _, err := f.provider().Exchange(ctx, "code", "verifier-verifier-verifier-verifier-verifier-1234", "n1"); err == nil {
				t.Fatal("token accepted")
			}
		})
	}

	t.Run("errors never echo tokens", func(t *testing.T) { // G18
		f := newFakeIDP(t)
		f.signWith = f.otherKey
		_, err := f.provider().Exchange(ctx, "code", "verifier-verifier-verifier-verifier-verifier-1234", "n1")
		if err == nil || strings.Contains(err.Error(), "access-secret") || strings.Contains(err.Error(), "refresh-secret") {
			t.Fatalf("err = %v", err)
		}
	})
}
