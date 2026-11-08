// Package store keeps accounts, login identities, sessions and sign-in attempts in SQLite.
package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"time"

	_ "modernc.org/sqlite" // pure-Go driver, no C compiler needed
)

// Store wraps the database.
type Store struct {
	db  *sql.DB
	now func() time.Time
}

// Open opens (and migrates) the database at path. now supplies the clock.
func Open(path string, now func() time.Time) (*Store, error) {
	q := url.Values{}
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "foreign_keys(1)")
	db, err := sql.Open("sqlite", "file:"+path+"?"+q.Encode())
	if err != nil {
		return nil, err
	}
	// One writer at a time (decision D11). This also serializes racing sign-ins.
	db.SetMaxOpenConns(1)
	s := &Store{db: db, now: now}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

const schema = `
CREATE TABLE IF NOT EXISTS accounts (
	id INTEGER PRIMARY KEY,
	created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS login_identities (
	id INTEGER PRIMARY KEY,
	account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
	provider TEXT NOT NULL,
	subject TEXT NOT NULL,
	email TEXT NOT NULL,
	name TEXT NOT NULL,
	UNIQUE (provider, subject)
);
CREATE TABLE IF NOT EXISTS sessions (
	id INTEGER PRIMARY KEY,
	token_hash TEXT NOT NULL UNIQUE,
	account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
	created_at INTEGER NOT NULL,
	last_seen_at INTEGER NOT NULL,
	idle_expires_at INTEGER NOT NULL,
	abs_expires_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS sign_in_attempts (
	id INTEGER PRIMARY KEY,
	state TEXT NOT NULL UNIQUE,
	nonce TEXT NOT NULL,
	verifier TEXT NOT NULL,
	binding_hash TEXT NOT NULL,
	expires_at INTEGER NOT NULL,
	consumed INTEGER NOT NULL DEFAULT 0
);
`

// HashToken returns the hex SHA-256 of a token. Only hashes reach the database.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// Claims are the only data kept from Google at sign-in (guarantee G3).
type Claims struct {
	Subject string
	Email   string
	Name    string
}

// Attempt is a consumed sign-in attempt.
type Attempt struct {
	Nonce    string
	Verifier string
}

// ErrNoAttempt means no matching, unexpired, unconsumed attempt exists.
var ErrNoAttempt = errors.New("no matching sign-in attempt")

// CreateAttempt stores a single-use sign-in attempt that lasts ttl.
func (s *Store) CreateAttempt(ctx context.Context, state, nonce, verifier, bindingToken string, ttl time.Duration) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sign_in_attempts (state, nonce, verifier, binding_hash, expires_at) VALUES (?, ?, ?, ?, ?)`,
		state, nonce, verifier, HashToken(bindingToken), s.now().Add(ttl).Unix())
	return err
}

// ConsumeAttempt marks the attempt used and returns it. The update is atomic,
// so an attempt completes at most once (G13).
func (s *Store) ConsumeAttempt(ctx context.Context, state, bindingToken string) (Attempt, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Attempt{}, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx,
		`UPDATE sign_in_attempts SET consumed = 1
		 WHERE state = ? AND binding_hash = ? AND consumed = 0 AND expires_at > ?`,
		state, HashToken(bindingToken), s.now().Unix())
	if err != nil {
		return Attempt{}, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return Attempt{}, ErrNoAttempt
	}
	var a Attempt
	if err := tx.QueryRowContext(ctx, `SELECT nonce, verifier FROM sign_in_attempts WHERE state = ?`, state).
		Scan(&a.Nonce, &a.Verifier); err != nil {
		return Attempt{}, err
	}
	return a, tx.Commit()
}

// UpsertAccount finds the account for a Google subject, or creates it with its
// login identity in one transaction. Email and name are refreshed (G4, G5).
func (s *Store) UpsertAccount(ctx context.Context, provider string, c Claims) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var accountID int64
	err = tx.QueryRowContext(ctx,
		`SELECT account_id FROM login_identities WHERE provider = ? AND subject = ?`, provider, c.Subject).
		Scan(&accountID)
	switch {
	case err == nil:
		if _, err := tx.ExecContext(ctx,
			`UPDATE login_identities SET email = ?, name = ? WHERE provider = ? AND subject = ?`,
			c.Email, c.Name, provider, c.Subject); err != nil {
			return 0, err
		}
	case errors.Is(err, sql.ErrNoRows):
		res, err := tx.ExecContext(ctx, `INSERT INTO accounts (created_at) VALUES (?)`, s.now().Unix())
		if err != nil {
			return 0, err
		}
		if accountID, err = res.LastInsertId(); err != nil {
			return 0, err
		}
		// UNIQUE (provider, subject) rejects a racing duplicate.
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO login_identities (account_id, provider, subject, email, name) VALUES (?, ?, ?, ?, ?)`,
			accountID, provider, c.Subject, c.Email, c.Name); err != nil {
			return 0, err
		}
	default:
		return 0, err
	}
	return accountID, tx.Commit()
}

// Account is the signed-in user's display data.
type Account struct {
	ID    int64
	Email string
	Name  string
}

// Session is a resolved, valid session.
type Session struct {
	Account Account
}

// CreateSession stores a session for token. Only the token hash is kept (G8).
func (s *Store) CreateSession(ctx context.Context, token string, accountID int64, idle, absolute time.Duration) error {
	now := s.now()
	idleExp := now.Add(idle)
	absExp := now.Add(absolute)
	if idleExp.After(absExp) {
		idleExp = absExp
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (token_hash, account_id, created_at, last_seen_at, idle_expires_at, abs_expires_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		HashToken(token), accountID, now.Unix(), now.Unix(), idleExp.Unix(), absExp.Unix())
	return err
}

// ErrNoSession means the token matches no valid session.
var ErrNoSession = errors.New("no valid session")

// refreshAfter limits last-seen writes to one per hour.
const refreshAfter = time.Hour

// LookupSession resolves a token. Past idle or absolute expiry counts as no
// session (G16). It slides the idle window at most once per hour.
func (s *Store) LookupSession(ctx context.Context, token string, idle time.Duration) (Session, error) {
	var (
		sess                      Session
		id, lastSeen, idleExp, ab int64
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT s.id, s.last_seen_at, s.idle_expires_at, s.abs_expires_at, a.id, i.email, i.name
		 FROM sessions s
		 JOIN accounts a ON a.id = s.account_id
		 JOIN login_identities i ON i.account_id = a.id
		 WHERE s.token_hash = ?`, HashToken(token)).
		Scan(&id, &lastSeen, &idleExp, &ab, &sess.Account.ID, &sess.Account.Email, &sess.Account.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrNoSession
	}
	if err != nil {
		return Session{}, err
	}
	now := s.now()
	if !now.Before(time.Unix(idleExp, 0)) || !now.Before(time.Unix(ab, 0)) {
		return Session{}, ErrNoSession
	}
	if now.Sub(time.Unix(lastSeen, 0)) >= refreshAfter {
		newIdle := now.Add(idle)
		if abs := time.Unix(ab, 0); newIdle.After(abs) {
			newIdle = abs
		}
		if _, err := s.db.ExecContext(ctx,
			`UPDATE sessions SET last_seen_at = ?, idle_expires_at = ? WHERE id = ?`,
			now.Unix(), newIdle.Unix(), id); err != nil {
			return Session{}, err
		}
	}
	return sess, nil
}

// DeleteSession removes the session for token. Only that session ends.
func (s *Store) DeleteSession(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, HashToken(token))
	return err
}

// SessionHashes lists stored token hashes (used by tests for G8).
func (s *Store) SessionHashes(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT token_hash FROM sessions`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// CountAccounts returns the number of accounts.
func (s *Store) CountAccounts(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM accounts`).Scan(&n)
	return n, err
}

// PurgeExpired deletes expired sessions and sign-in attempts.
func (s *Store) PurgeExpired(ctx context.Context) error {
	now := s.now().Unix()
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM sessions WHERE idle_expires_at <= ? OR abs_expires_at <= ?`, now, now); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM sign_in_attempts WHERE expires_at <= ? OR consumed = 1`, now)
	return err
}
