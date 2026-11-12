// Package gmail is the one place allowed to call the Gmail API (decision D11).
//
// It allows only account-profile and metadata-level requests: headers, labels, IDs
// and the snippet. It refuses full, raw and attachment requests, because Geronimo
// promises never to read message bodies. Its one change to mail is MarkRead, which
// removes the UNREAD label from one message and nothing else. Every later epic that reads Gmail must go
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

// MetadataHeaders are the only headers a metadata read asks for (decision D3).
var MetadataHeaders = []string{"From", "To", "Subject", "Date", "List-Id", "List-Unsubscribe"}

// Profile is the account-level data returned by the profile request.
type Profile struct {
	Email string `json:"emailAddress"`
}

// Counts are Gmail's message totals for one label.
type Counts struct {
	Total  int `json:"messagesTotal"`
	Unread int `json:"messagesUnread"`
}

// MessagePage is one page of the inbox message list, newest first. Next is empty on the last page.
type MessagePage struct {
	IDs  []string
	Next string
}

// Message is the metadata of one message. It holds no body and no snippet (G12).
type Message struct {
	ID              string
	ThreadID        string
	Labels          []string
	From            string
	To              string
	Subject         string
	Date            string
	ListID          string
	ListUnsubscribe string
}

// StatusError is a Gmail answer other than 200. It carries the status code only, never the body (G21).
type StatusError struct {
	Code int
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("gmail: status %d", e.Code)
}

// IsRateLimited reports whether Gmail answered with a quota or rate-limit status.
// Gmail sends quota errors as 429 or as 403.
func IsRateLimited(err error) bool {
	var se *StatusError
	return errors.As(err, &se) && (se.Code == http.StatusTooManyRequests || se.Code == http.StatusForbidden)
}

// IsNotFound reports whether Gmail answered that the requested object does not exist.
func IsNotFound(err error) bool {
	var se *StatusError
	return errors.As(err, &se) && se.Code == http.StatusNotFound
}

// IsBadRequest reports whether Gmail rejected the request as malformed, such as a stale page token.
func IsBadRequest(err error) bool {
	var se *StatusError
	return errors.As(err, &se) && se.Code == http.StatusBadRequest
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

// InboxCounts reads the inbox label's total and unread message counts (decision D4).
func (c *Client) InboxCounts(ctx context.Context, accessToken string) (Counts, error) {
	b, err := c.Get(ctx, accessToken, "/users/me/labels/INBOX", nil)
	if err != nil {
		return Counts{}, err
	}
	var n Counts
	if err := json.Unmarshal(b, &n); err != nil {
		return Counts{}, errors.New("gmail: unreadable label response")
	}
	return n, nil
}

// ListInbox lists one page of inbox message IDs, newest first (decision D2).
func (c *Client) ListInbox(ctx context.Context, accessToken, pageToken string) (MessagePage, error) {
	q := url.Values{"labelIds": {"INBOX"}, "maxResults": {"100"}}
	if pageToken != "" {
		q.Set("pageToken", pageToken)
	}
	b, err := c.Get(ctx, accessToken, "/users/me/messages", q)
	if err != nil {
		return MessagePage{}, err
	}
	var raw struct {
		Messages []struct {
			ID string `json:"id"`
		} `json:"messages"`
		NextPageToken string `json:"nextPageToken"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return MessagePage{}, errors.New("gmail: unreadable message list")
	}
	page := MessagePage{Next: raw.NextPageToken}
	for _, m := range raw.Messages {
		page.IDs = append(page.IDs, m.ID)
	}
	return page, nil
}

// Metadata reads one message's headers, labels and IDs in metadata format (decision D3).
func (c *Client) Metadata(ctx context.Context, accessToken, id string) (Message, error) {
	q := url.Values{"format": {"metadata"}}
	for _, h := range MetadataHeaders {
		q.Add("metadataHeaders", h)
	}
	b, err := c.Get(ctx, accessToken, "/users/me/messages/"+id, q)
	if err != nil {
		return Message{}, err
	}
	var raw struct {
		ID       string   `json:"id"`
		ThreadID string   `json:"threadId"`
		LabelIDs []string `json:"labelIds"`
		Payload  struct {
			Headers []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			} `json:"headers"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return Message{}, errors.New("gmail: unreadable message")
	}
	m := Message{ID: raw.ID, ThreadID: raw.ThreadID, Labels: raw.LabelIDs}
	for _, h := range raw.Payload.Headers {
		switch {
		case strings.EqualFold(h.Name, "From"):
			m.From = h.Value
		case strings.EqualFold(h.Name, "To"):
			m.To = h.Value
		case strings.EqualFold(h.Name, "Subject"):
			m.Subject = h.Value
		case strings.EqualFold(h.Name, "Date"):
			m.Date = h.Value
		case strings.EqualFold(h.Name, "List-Id"):
			m.ListID = h.Value
		case strings.EqualFold(h.Name, "List-Unsubscribe"):
			m.ListUnsubscribe = h.Value
		}
	}
	return m, nil
}

// WebURL is the address that opens one message in the Gmail web UI, in the signed-in
// Google account whose address is mailbox (#43).
func WebURL(mailbox, messageID string) string {
	return "https://mail.google.com/mail/?authuser=" + url.QueryEscape(mailbox) + "#all/" + url.PathEscape(messageID)
}

// markReadBody removes the UNREAD label and changes no other label.
const markReadBody = `{"removeLabelIds":["UNREAD"]}`

// MarkRead removes the UNREAD label from one message (#43). It is the access point's only write.
func (c *Client) MarkRead(ctx context.Context, accessToken, id string) error {
	if id == "" || strings.ContainsAny(id, "/?#%\\") || strings.Contains(id, "..") {
		return errors.New("gmail: request refused")
	}
	_, err := c.send(ctx, http.MethodPost, accessToken, c.base+"/users/me/messages/"+id+"/modify", strings.NewReader(markReadBody))
	return err
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
	return c.send(ctx, http.MethodGet, accessToken, u, nil)
}

// send is the one place a request leaves for Gmail.
func (c *Client) send(ctx context.Context, method, accessToken, u string, body io.Reader) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, errors.New("gmail: cannot build request")
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, errors.New("gmail: request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &StatusError{Code: resp.StatusCode}
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
	case len(parts) == 2 && parts[0] == "labels" && parts[1] == "INBOX":
		// The inbox label's counts only (decision D4).
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
