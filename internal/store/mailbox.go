package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sameera/geronimo/internal/secretbox"
)

// Mailbox statuses.
const (
	StatusConnected = "connected"
)

var (
	// ErrNoTokenKey means no token key was set, so no token can be stored or read.
	ErrNoTokenKey = errors.New("no token key set")
	// ErrUndecryptable means a stored token cannot be opened with the current key.
	ErrUndecryptable = errors.New("stored token cannot be decrypted")
	// ErrNoMailbox means no such connected mailbox exists.
	ErrNoMailbox = errors.New("no such mailbox")
)

// SetTokenKey sets the key that encrypts refresh tokens at rest (decision D6).
func (s *Store) SetTokenKey(key []byte) error {
	if len(key) != secretbox.KeySize {
		return fmt.Errorf("token key must be %d bytes", secretbox.KeySize)
	}
	s.tokenKey = append([]byte(nil), key...)
	return nil
}

// Mailbox is a Gmail account to link to a Geronimo account.
type Mailbox struct {
	Subject      string
	Email        string
	RefreshToken string
	Scopes       []string
}

// MailboxInfo is what the home screen shows for a connected mailbox.
type MailboxInfo struct {
	Email  string
	Status string
}

// binding ties a ciphertext to its record, so it cannot be moved to another row.
func binding(accountID int64, subject string) []byte {
	return []byte(fmt.Sprintf("mailbox:%d:%s", accountID, subject))
}

// SaveOutcome says what SaveMailbox did.
type SaveOutcome int

const (
	// Added means the Gmail account was not connected before.
	Added SaveOutcome = iota + 1
	// Replaced means the Gmail account was already connected and its token was replaced.
	Replaced
)

// SaveMailbox links a Gmail account to a Geronimo account, keyed by (account, Google
// subject). A repeat connect adds no row: it replaces the stored token, scopes and
// email, and sets the status to connected (decision D5). The old token is never
// revoked. The refresh token is stored only encrypted.
func (s *Store) SaveMailbox(ctx context.Context, accountID int64, m Mailbox) (SaveOutcome, error) {
	if s.tokenKey == nil {
		return 0, ErrNoTokenKey
	}
	enc, err := secretbox.Seal(s.tokenKey, binding(accountID, m.Subject), []byte(m.RefreshToken))
	if err != nil {
		return 0, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var id int64
	err = tx.QueryRowContext(ctx,
		`SELECT id FROM mailboxes WHERE account_id = ? AND google_subject = ?`, accountID, m.Subject).Scan(&id)
	outcome := Replaced
	switch {
	case err == nil:
		_, err = tx.ExecContext(ctx,
			`UPDATE mailboxes SET email = ?, refresh_token_enc = ?, scopes = ?, status = ?, last_checked_at = ? WHERE id = ?`,
			m.Email, enc, strings.Join(m.Scopes, " "), StatusConnected, s.now().Unix(), id)
	case errors.Is(err, sql.ErrNoRows):
		outcome = Added
		// UNIQUE (account_id, google_subject) rejects a racing duplicate.
		_, err = tx.ExecContext(ctx,
			`INSERT INTO mailboxes (account_id, google_subject, email, refresh_token_enc, scopes, status, last_checked_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			accountID, m.Subject, m.Email, enc, strings.Join(m.Scopes, " "), StatusConnected, s.now().Unix())
	}
	if err != nil {
		return 0, err
	}
	return outcome, tx.Commit()
}

// Mailboxes lists an account's connected mailboxes, oldest first.
func (s *Store) Mailboxes(ctx context.Context, accountID int64) ([]MailboxInfo, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT email, status FROM mailboxes WHERE account_id = ? ORDER BY id`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MailboxInfo
	for rows.Next() {
		var m MailboxInfo
		if err := rows.Scan(&m.Email, &m.Status); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// RefreshToken decrypts a mailbox's refresh token. A token that cannot be opened
// gives ErrUndecryptable.
func (s *Store) RefreshToken(ctx context.Context, accountID int64, subject string) (string, error) {
	if s.tokenKey == nil {
		return "", ErrNoTokenKey
	}
	var enc []byte
	err := s.db.QueryRowContext(ctx,
		`SELECT refresh_token_enc FROM mailboxes WHERE account_id = ? AND google_subject = ?`, accountID, subject).
		Scan(&enc)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNoMailbox
	}
	if err != nil {
		return "", err
	}
	pt, err := secretbox.Open(s.tokenKey, binding(accountID, subject), enc)
	if err != nil {
		return "", ErrUndecryptable
	}
	return string(pt), nil
}

// ConnectAttempt is a consumed connect attempt.
type ConnectAttempt struct {
	AccountID int64
	Nonce     string
	Verifier  string
}

// CreateConnectAttempt stores a single-use connect attempt bound to the browser and
// the Geronimo account that started it (decision D3). It is a separate kind from a
// sign-in attempt, so neither can finish the other.
func (s *Store) CreateConnectAttempt(ctx context.Context, state, nonce, verifier, bindingToken string, accountID int64, ttl time.Duration) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO connect_attempts (state, nonce, verifier, binding_hash, account_id, expires_at) VALUES (?, ?, ?, ?, ?, ?)`,
		state, nonce, verifier, HashToken(bindingToken), accountID, s.now().Add(ttl).Unix())
	return err
}

// ConsumeConnectAttempt marks the attempt used and returns it, atomically.
func (s *Store) ConsumeConnectAttempt(ctx context.Context, state, bindingToken string) (ConnectAttempt, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ConnectAttempt{}, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx,
		`UPDATE connect_attempts SET consumed = 1
		 WHERE state = ? AND binding_hash = ? AND consumed = 0 AND expires_at > ?`,
		state, HashToken(bindingToken), s.now().Unix())
	if err != nil {
		return ConnectAttempt{}, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return ConnectAttempt{}, ErrNoAttempt
	}
	var a ConnectAttempt
	if err := tx.QueryRowContext(ctx, `SELECT account_id, nonce, verifier FROM connect_attempts WHERE state = ?`, state).
		Scan(&a.AccountID, &a.Nonce, &a.Verifier); err != nil {
		return ConnectAttempt{}, err
	}
	return a, tx.Commit()
}

// CountConnectAttempts returns the number of stored connect attempts (used by tests).
func (s *Store) CountConnectAttempts(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM connect_attempts`).Scan(&n)
	return n, err
}
