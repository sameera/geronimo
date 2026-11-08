package auth

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/sameera/geronimo/internal/gmail"
)

// Grant is what a completed Gmail consent gives Geronimo.
type Grant struct {
	Subject      string
	Email        string
	RefreshToken string
	AccessToken  string
	Scopes       []string // the scopes Google reports as granted
}

// Connector is Google's Gmail consent step, separate from sign-in (decision D2).
type Connector interface {
	// AuthURL returns the address to send the user to.
	AuthURL(state, nonce, verifier string) (string, error)
	// Exchange trades the code for a verified identity, tokens and the granted scopes.
	Exchange(ctx context.Context, code, verifier, nonce string) (Grant, error)
}

// GoogleConnect is the Google Gmail consent step.
type GoogleConnect struct {
	issuer       string
	clientID     string
	clientSecret string
	redirectURL  string

	mu       sync.Mutex
	provider *oidc.Provider
}

// NewGoogleConnect builds the connector. issuer is GoogleIssuer in production.
func NewGoogleConnect(issuer, clientID, clientSecret, redirectURL string) *GoogleConnect {
	return &GoogleConnect{issuer: issuer, clientID: clientID, clientSecret: clientSecret, redirectURL: redirectURL}
}

func (g *GoogleConnect) discover(ctx context.Context) (*oidc.Provider, error) {
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

// oauthConfig asks for Gmail modify plus openid and email, and no other Gmail scope (G1).
func (g *GoogleConnect) oauthConfig(p *oidc.Provider) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     g.clientID,
		ClientSecret: g.clientSecret,
		RedirectURL:  g.redirectURL,
		Endpoint:     p.Endpoint(),
		Scopes:       []string{oidc.ScopeOpenID, "email", gmail.ModifyScope},
	}
}

func (g *GoogleConnect) AuthURL(state, nonce, verifier string) (string, error) {
	p, err := g.discover(context.Background())
	if err != nil {
		return "", fmt.Errorf("discovery: %w", err)
	}
	// Offline access plus a forced consent prompt make Google return a refresh token
	// on every connect. The account chooser lets the user pick the mailbox.
	return g.oauthConfig(p).AuthCodeURL(state,
		oauth2.S256ChallengeOption(verifier),
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("nonce", nonce),
		oauth2.SetAuthURLParam("prompt", "consent select_account"),
	), nil
}

func (g *GoogleConnect) Exchange(ctx context.Context, code, verifier, nonce string) (Grant, error) {
	p, err := g.discover(ctx)
	if err != nil {
		return Grant{}, fmt.Errorf("discovery: %w", err)
	}
	tok, err := g.oauthConfig(p).Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return Grant{}, fmt.Errorf("code exchange: %s", safeErr(err))
	}
	raw, _ := tok.Extra("id_token").(string)
	if raw == "" {
		return Grant{}, errors.New("code exchange: no id_token in response")
	}
	idToken, err := p.Verifier(&oidc.Config{ClientID: g.clientID}).Verify(ctx, raw)
	if err != nil {
		return Grant{}, fmt.Errorf("id token: %w", err)
	}
	if idToken.Nonce == "" || idToken.Nonce != nonce {
		return Grant{}, errors.New("id token: nonce mismatch")
	}
	if idToken.Subject == "" {
		return Grant{}, errors.New("id token: empty subject")
	}
	var c struct {
		Email string `json:"email"`
	}
	if err := idToken.Claims(&c); err != nil {
		return Grant{}, fmt.Errorf("id token claims: %w", err)
	}
	if tok.RefreshToken == "" {
		return Grant{}, errors.New("code exchange: no refresh token in response")
	}
	scope, _ := tok.Extra("scope").(string)
	return Grant{
		Subject:      idToken.Subject,
		Email:        c.Email,
		RefreshToken: tok.RefreshToken,
		AccessToken:  tok.AccessToken,
		Scopes:       strings.Fields(scope),
	}, nil
}

// ErrAccessRemoved means Google answered that the grant is gone: an explicit
// invalid_grant, or a refreshed token that no longer carries the Gmail scope.
var ErrAccessRemoved = errors.New("access was removed at Google")

// Checker tests that a stored refresh token still works at Google (decision D8).
type Checker interface {
	// Check returns ErrAccessRemoved only for Google's explicit answer. Any other
	// error means "unknown": timeouts, network trouble and server errors.
	Check(ctx context.Context, refreshToken string) error
}

// Check refreshes an access token. A refresh is a token-endpoint call, not a Gmail
// call, so it carries no message-body risk. The new access token is dropped at once.
func (g *GoogleConnect) Check(ctx context.Context, refreshToken string) error {
	p, err := g.discover(ctx)
	if err != nil {
		return fmt.Errorf("discovery: %w", err)
	}
	tok, err := g.oauthConfig(p).TokenSource(ctx, &oauth2.Token{RefreshToken: refreshToken}).Token()
	if err != nil {
		var re *oauth2.RetrieveError
		if errors.As(err, &re) && re.ErrorCode == "invalid_grant" {
			return ErrAccessRemoved
		}
		return fmt.Errorf("refresh: %s", safeErr(err))
	}
	// A scope list that no longer has Gmail modify means the user took it back.
	if scope, _ := tok.Extra("scope").(string); scope != "" && !slices.Contains(strings.Fields(scope), gmail.ModifyScope) {
		return ErrAccessRemoved
	}
	return nil
}
