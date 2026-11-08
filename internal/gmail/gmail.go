// Package gmail is the one place allowed to call the Gmail API (decision D11).
//
// It allows only account-profile and metadata-level requests: headers, labels, IDs
// and the snippet. It refuses full, raw and attachment requests, because Geronimo
// promises never to read message bodies. Every later epic that reads Gmail must go
// through this package. A test fails if any other code names the Gmail API.
package gmail

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// ModifyScope is the one Gmail scope Geronimo asks for (decision D1).
const ModifyScope = "https://www.googleapis.com/auth/gmail.modify"

// DefaultBaseURL is the Gmail API root.
const DefaultBaseURL = "https://gmail.googleapis.com/gmail/v1"

// Profile is the account-level data returned by the profile request.
type Profile struct {
	Email string `json:"emailAddress"`
}

// Client sends the allowed requests.
type Client struct {
	base string
	hc   *http.Client
}

// New builds a Client. A nil hc means http.DefaultClient.
func New(baseURL string, hc *http.Client) *Client {
	if hc == nil {
		hc = http.DefaultClient
	}
	return &Client{base: strings.TrimRight(baseURL, "/"), hc: hc}
}

// Profile makes the account profile request.
func (c *Client) Profile(ctx context.Context, accessToken string) (Profile, error) {
	b, err := c.Get(ctx, accessToken, "/users/me/profile", nil)
	if err != nil {
		return Profile{}, err
	}
	var p Profile
	if err := json.Unmarshal(b, &p); err != nil {
		return Profile{}, errors.New("gmail: unreadable profile response")
	}
	if p.Email == "" {
		return Profile{}, errors.New("gmail: profile has no address")
	}
	return p, nil
}

// Get sends one allowed GET request and returns the response body. Errors carry the
// status code only, never a response body or the token.
func (c *Client) Get(ctx context.Context, accessToken, path string, q url.Values) ([]byte, error) {
	if err := check(path, q); err != nil {
		return nil, err
	}
	u := c.base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, errors.New("gmail: cannot build request")
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, errors.New("gmail: request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gmail: status %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, errors.New("gmail: unreadable response")
	}
	return b, nil
}

// check applies the allowlist. Anything not listed is refused.
func check(path string, q url.Values) error {
	if !strings.HasPrefix(path, "/users/me/") || strings.ContainsAny(path, "?#%\\") || strings.Contains(path, "..") || strings.Contains(path, "//") {
		return errors.New("gmail: request refused")
	}
	rest := strings.TrimPrefix(path, "/users/me/")
	parts := strings.Split(rest, "/")
	switch {
	case len(parts) == 1 && (parts[0] == "profile" || parts[0] == "labels" || parts[0] == "history"):
		return noFormat(q)
	case len(parts) == 1 && parts[0] == "messages":
		return noFormat(q)
	case len(parts) == 2 && parts[0] == "messages" && parts[1] != "":
		// Gmail returns the full message when no format is given, so a format is required.
		f := q["format"]
		if len(f) == 1 && (f[0] == "metadata" || f[0] == "minimal") {
			return nil
		}
		return errors.New("gmail: message requests must use the metadata or minimal format")
	}
	return errors.New("gmail: request refused")
}

func noFormat(q url.Values) error {
	if _, ok := q["format"]; ok {
		return errors.New("gmail: request refused")
	}
	return nil
}
