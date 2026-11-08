package auth

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/sameera/geronimo/internal/store"
)

// ProviderName is the identity provider recorded on every login identity.
const ProviderName = "google"

// GoogleIssuer is Google's OpenID Connect issuer.
const GoogleIssuer = "https://accounts.google.com"

// Provider is the identity provider side of sign-in.
type Provider interface {
	// AuthURL returns the address to send the visitor to.
	AuthURL(state, nonce, verifier string) (string, error)
	// Exchange trades the code for a validated identity. It keeps no token.
	Exchange(ctx context.Context, code, verifier, nonce string) (store.Claims, error)
}

// Google is the Google OpenID Connect provider.
type Google struct {
	issuer       string
	clientID     string
	clientSecret string
	redirectURL  string

	mu       sync.Mutex
	provider *oidc.Provider
}

// NewGoogle builds a provider. issuer is GoogleIssuer in production.
func NewGoogle(issuer, clientID, clientSecret, redirectURL string) *Google {
	return &Google{issuer: issuer, clientID: clientID, clientSecret: clientSecret, redirectURL: redirectURL}
}

// discover loads Google's endpoints on first use and retries after a failure.
func (g *Google) discover(ctx context.Context) (*oidc.Provider, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.provider != nil {
		return g.provider, nil
	}
	p, err := oidc.NewProvider(ctx, g.issuer)
	if err != nil {
		return nil, err
	}
	g.provider = p
	return p, nil
}

// oauthConfig asks for identity only: openid, email, profile (G1).
func (g *Google) oauthConfig(p *oidc.Provider) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     g.clientID,
		ClientSecret: g.clientSecret,
		RedirectURL:  g.redirectURL,
		Endpoint:     p.Endpoint(),
		Scopes:       []string{oidc.ScopeOpenID, "email", "profile"},
	}
}

func (g *Google) AuthURL(state, nonce, verifier string) (string, error) {
	p, err := g.discover(context.Background())
	if err != nil {
		return "", fmt.Errorf("discovery: %w", err)
	}
	// No access_type=offline: no refresh token is requested (G1).
	return g.oauthConfig(p).AuthCodeURL(state,
		oauth2.S256ChallengeOption(verifier),
		oauth2.SetAuthURLParam("nonce", nonce),
		oauth2.SetAuthURLParam("prompt", "select_account"),
	), nil
}

func (g *Google) Exchange(ctx context.Context, code, verifier, nonce string) (store.Claims, error) {
	p, err := g.discover(ctx)
	if err != nil {
		return store.Claims{}, fmt.Errorf("discovery: %w", err)
	}
	tok, err := g.oauthConfig(p).Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return store.Claims{}, fmt.Errorf("code exchange: %s", safeErr(err))
	}
	raw, _ := tok.Extra("id_token").(string)
	if raw == "" {
		return store.Claims{}, errors.New("code exchange: no id_token in response")
	}
	// Checks signature, issuer, audience and expiry.
	idToken, err := p.Verifier(&oidc.Config{ClientID: g.clientID}).Verify(ctx, raw)
	if err != nil {
		return store.Claims{}, fmt.Errorf("id token: %w", err)
	}
	if idToken.Nonce == "" || idToken.Nonce != nonce {
		return store.Claims{}, errors.New("id token: nonce mismatch")
	}
	var c struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	if err := idToken.Claims(&c); err != nil {
		return store.Claims{}, fmt.Errorf("id token claims: %w", err)
	}
	if idToken.Subject == "" {
		return store.Claims{}, errors.New("id token: empty subject")
	}
	// The oauth2 token (access, refresh, ID token) goes out of scope here and is never stored (G2).
	return store.Claims{Subject: idToken.Subject, Email: c.Email, Name: c.Name}, nil
}

// safeErr describes an error without echoing response bodies, which could carry codes or tokens (G18).
func safeErr(err error) string {
	var re *oauth2.RetrieveError
	if errors.As(err, &re) {
		return fmt.Sprintf("provider returned status %d error %q", re.Response.StatusCode, re.ErrorCode)
	}
	return "request failed"
}
