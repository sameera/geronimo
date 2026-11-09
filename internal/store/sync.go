package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// Sync states (decision D1).
const (
	SyncStarting = "starting"
	SyncRunning  = "running"
	SyncStopped  = "stopped"
	SyncDone     = "done"
)

// Stop reasons. A stopped sync shows one of these (decision D5).
const (
	StopAccessRemoved = "access_removed"
	StopRateLimited   = "rate_limited"
	StopOther         = "other"
)

// syncSchema holds the first-sync tables. Every table hangs off a mailbox, so removing
// the account removes all of it (G19).
const syncSchema = `
CREATE TABLE IF NOT EXISTS sync_records (
	mailbox_id INTEGER PRIMARY KEY REFERENCES mailboxes(id) ON DELETE CASCADE,
	state TEXT NOT NULL,
	stop_reason TEXT NOT NULL DEFAULT '',
	inbox_total INTEGER NOT NULL DEFAULT 0,
	synced_count INTEGER NOT NULL DEFAULT 0,
	list_cursor TEXT NOT NULL DEFAULT '',
	listed_all INTEGER NOT NULL DEFAULT 0,
	finished_at INTEGER NOT NULL DEFAULT 0,
	strip_acked INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS pending_messages (
	mailbox_id INTEGER NOT NULL REFERENCES mailboxes(id) ON DELETE CASCADE,
	message_id TEXT NOT NULL,
	PRIMARY KEY (mailbox_id, message_id)
);
CREATE TABLE IF NOT EXISTS message_records (
	mailbox_id INTEGER NOT NULL REFERENCES mailboxes(id) ON DELETE CASCADE,
	message_id TEXT NOT NULL,
	thread_id TEXT NOT NULL,
	sender TEXT NOT NULL,
	recipients TEXT NOT NULL,
	subject TEXT NOT NULL,
	date TEXT NOT NULL,
	list_id TEXT NOT NULL,
	list_unsubscribe TEXT NOT NULL,
	labels TEXT NOT NULL,
	PRIMARY KEY (mailbox_id, message_id)
);
CREATE TABLE IF NOT EXISTS baselines (
	mailbox_id INTEGER PRIMARY KEY REFERENCES mailboxes(id) ON DELETE CASCADE,
	unread INTEGER NOT NULL,
	recorded_at INTEGER NOT NULL
);
`

// SyncRecord is one mailbox's first sync.
type SyncRecord struct {
	MailboxID   int64
	AccountID   int64
	Subject     string
	Email       string
	State       string
	StopReason  string
	InboxTotal  int
	SyncedCount int
	Cursor      string
	ListedAll   bool
	StripAcked  bool
	FinishedAt  int64
}

// Message is the stored metadata of one email. It has no body and no snippet (G12).
type Message struct {
	ID              string
	ThreadID        string
	Sender          string
	Recipients      string
	Subject         string
	Date            string
	ListID          string
	ListUnsubscribe string
	Labels          []string
}

// MailboxID returns the id of an account's mailbox for a Google subject.
func (s *Store) MailboxID(ctx context.Context, accountID int64, subject string) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx,
		`SELECT id FROM mailboxes WHERE account_id = ? AND google_subject = ?`, accountID, subject).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNoMailbox
	}
	return id, err
}

// EnsureSync creates the mailbox's sync record in state starting. A stopped record goes
// back to starting and keeps its stored mail. Running, starting and done records are left alone (D6, D5).
func (s *Store) EnsureSync(ctx context.Context, mailboxID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO sync_records (mailbox_id, state) VALUES (?, ?)`, mailboxID, SyncStarting); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE sync_records SET state = ?, stop_reason = '' WHERE mailbox_id = ? AND state = ?`,
		SyncStarting, mailboxID, SyncStopped); err != nil {
		return err
	}
	return tx.Commit()
}

const syncColumns = `r.mailbox_id, m.account_id, m.google_subject, m.email, r.state, r.stop_reason,
	r.inbox_total, r.synced_count, r.list_cursor, r.listed_all, r.strip_acked, r.finished_at`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanSync(row rowScanner) (SyncRecord, error) {
	var (
		r                     SyncRecord
		listedAll, stripAcked int
	)
	err := row.Scan(&r.MailboxID, &r.AccountID, &r.Subject, &r.Email, &r.State, &r.StopReason,
		&r.InboxTotal, &r.SyncedCount, &r.Cursor, &listedAll, &stripAcked, &r.FinishedAt)
	r.ListedAll = listedAll == 1
	r.StripAcked = stripAcked == 1
	return r, err
}

// SyncFor returns one mailbox's sync record.
func (s *Store) SyncFor(ctx context.Context, mailboxID int64) (SyncRecord, error) {
	r, err := scanSync(s.db.QueryRowContext(ctx,
		`SELECT `+syncColumns+` FROM sync_records r JOIN mailboxes m ON m.id = r.mailbox_id WHERE r.mailbox_id = ?`,
		mailboxID))
	if errors.Is(err, sql.ErrNoRows) {
		return SyncRecord{}, ErrNoSync
	}
	return r, err
}

// AccountSyncs lists the sync records of an account's mailboxes, oldest mailbox first.
func (s *Store) AccountSyncs(ctx context.Context, accountID int64) ([]SyncRecord, error) {
	return s.listSyncs(ctx,
		`SELECT `+syncColumns+` FROM sync_records r JOIN mailboxes m ON m.id = r.mailbox_id WHERE m.account_id = ? ORDER BY m.id`,
		accountID)
}

// ResumableSyncs lists the records a restarted service must continue: starting and running ones (G8).
func (s *Store) ResumableSyncs(ctx context.Context) ([]SyncRecord, error) {
	return s.listSyncs(ctx,
		`SELECT `+syncColumns+` FROM sync_records r JOIN mailboxes m ON m.id = r.mailbox_id WHERE r.state IN (?, ?) ORDER BY r.mailbox_id`,
		SyncStarting, SyncRunning)
}

func (s *Store) listSyncs(ctx context.Context, query string, args ...any) ([]SyncRecord, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SyncRecord
	for rows.Next() {
		r, err := scanSync(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// MarkStripAcked records that the done line of a mailbox's sync has been shown (G15).
func (s *Store) MarkStripAcked(ctx context.Context, mailboxID int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sync_records SET strip_acked = 1 WHERE mailbox_id = ?`, mailboxID)
	return err
}

// StartRun moves a starting sync to running and stores the inbox total read at its start (D7).
func (s *Store) StartRun(ctx context.Context, mailboxID int64, inboxTotal int) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE sync_records SET state = ?, inbox_total = ? WHERE mailbox_id = ? AND state = ?`,
		SyncRunning, inboxTotal, mailboxID, SyncStarting)
	return err
}

// StopSync stops a starting or running sync with a reason. Stored mail is kept (G5).
func (s *Store) StopSync(ctx context.Context, mailboxID int64, reason string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE sync_records SET state = ?, stop_reason = ? WHERE mailbox_id = ? AND state IN (?, ?)`,
		SyncStopped, reason, mailboxID, SyncStarting, SyncRunning)
	return err
}

// FinishSync marks a running sync done. In the same transaction it stores the baseline,
// the inbox unread count Gmail reported at this finish, unless one already exists (D8, G16, G17).
// A sync that is not running changes nothing, so a finished mailbox keeps its baseline (G18).
func (s *Store) FinishSync(ctx context.Context, mailboxID int64, unread int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := s.now().Unix()
	res, err := tx.ExecContext(ctx,
		`UPDATE sync_records SET state = ?, stop_reason = '', finished_at = ? WHERE mailbox_id = ? AND state = ?`,
		SyncDone, now, mailboxID, SyncRunning)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return nil
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO baselines (mailbox_id, unread, recorded_at) VALUES (?, ?, ?)`,
		mailboxID, unread, now); err != nil {
		return err
	}
	return tx.Commit()
}

// Baseline is an account's unread count, recorded once when its first sync finished.
type Baseline struct {
	Unread     int
	RecordedAt int64
}

// Baseline returns a mailbox's baseline, and false before its first sync finishes.
func (s *Store) Baseline(ctx context.Context, mailboxID int64) (Baseline, bool, error) {
	var b Baseline
	err := s.db.QueryRowContext(ctx,
		`SELECT unread, recorded_at FROM baselines WHERE mailbox_id = ?`, mailboxID).Scan(&b.Unread, &b.RecordedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Baseline{}, false, nil
	}
	if err != nil {
		return Baseline{}, false, err
	}
	return b, true, nil
}

// StoreListPage saves one listed page of inbox IDs as pending, with the listing cursor, in one transaction (D2).
func (s *Store) StoreListPage(ctx context.Context, mailboxID int64, ids []string, cursor string, listedAll bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO pending_messages (mailbox_id, message_id) VALUES (?, ?)`, mailboxID, id); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE sync_records SET list_cursor = ?, listed_all = ? WHERE mailbox_id = ?`,
		cursor, boolInt(listedAll), mailboxID); err != nil {
		return err
	}
	return tx.Commit()
}

// PendingIDs returns up to limit listed IDs whose metadata is not stored yet, in list order.
func (s *Store) PendingIDs(ctx context.Context, mailboxID int64, limit int) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT message_id FROM pending_messages WHERE mailbox_id = ? ORDER BY rowid LIMIT ?`, mailboxID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// StoreMessages stores one batch in one transaction. A message already stored is skipped,
// so a retry never stores an email twice (G7). IDs in gone no longer exist at Gmail and leave the pending list.
// It returns how many messages were newly stored.
func (s *Store) StoreMessages(ctx context.Context, mailboxID int64, msgs []Message, gone []string) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	inserted := 0
	for _, m := range msgs {
		res, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO message_records
				(mailbox_id, message_id, thread_id, sender, recipients, subject, date, list_id, list_unsubscribe, labels)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			mailboxID, m.ID, m.ThreadID, m.Sender, m.Recipients, m.Subject, m.Date, m.ListID, m.ListUnsubscribe,
			strings.Join(m.Labels, ","))
		if err != nil {
			return 0, err
		}
		n, _ := res.RowsAffected()
		inserted += int(n)
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM pending_messages WHERE mailbox_id = ? AND message_id = ?`, mailboxID, m.ID); err != nil {
			return 0, err
		}
	}
	for _, id := range gone {
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM pending_messages WHERE mailbox_id = ? AND message_id = ?`, mailboxID, id); err != nil {
			return 0, err
		}
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE sync_records SET synced_count = synced_count + ? WHERE mailbox_id = ?`, inserted, mailboxID); err != nil {
		return 0, err
	}
	return inserted, tx.Commit()
}

// MessageIDs lists the stored message IDs of a mailbox, in the order they were stored.
func (s *Store) MessageIDs(ctx context.Context, mailboxID int64) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT message_id FROM message_records WHERE mailbox_id = ? ORDER BY rowid`, mailboxID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ErrNoSync means the mailbox has no sync record.
var ErrNoSync = errors.New("no sync record")

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
