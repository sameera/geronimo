package store

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"net/mail"
	"strings"
	"time"
)

// UnknownDate is the stored date of an email whose Date header is missing or unreadable. It sorts last (D3).
const UnknownDate int64 = math.MinInt64

// UnknownSenderKey is the sender key of emails with an empty From header (D1). No parsed address can equal it.
const UnknownSenderKey = "\x00unknown"

// Derived holds the values worked out from an email's headers and labels each time a record is written (D1, D3).
type Derived struct {
	Key    string // lowercased address; identity of a sender group
	Name   string // display name, may be empty
	Date   int64  // unix seconds, or UnknownDate
	Unread bool   // carries Gmail's UNREAD label
}

// Derive is the one parsing routine, used by the write path and by the backfill.
func Derive(from, date string, labels []string) Derived {
	d := Derived{Date: UnknownDate}
	from = strings.TrimSpace(from)
	switch addr, err := mail.ParseAddress(from); {
	case err == nil && addr.Address != "":
		d.Key = strings.ToLower(addr.Address)
		d.Name = addr.Name
	case from == "":
		d.Key = UnknownSenderKey
	default:
		d.Key = strings.ToLower(from)
	}
	if t, err := mail.ParseDate(strings.TrimSpace(date)); err == nil {
		d.Date = t.Unix()
	}
	for _, l := range labels {
		if l == "UNREAD" {
			d.Unread = true
		}
	}
	return d
}

// senderSchema adds the derived fields to a table epic #4 created without them (R1).
var senderColumns = []struct{ name, def string }{
	{"sender_key", "TEXT NOT NULL DEFAULT ''"},
	{"sender_name", "TEXT NOT NULL DEFAULT ''"},
	{"date_ts", "INTEGER NOT NULL DEFAULT -9223372036854775808"},
	{"unread", "INTEGER NOT NULL DEFAULT 0"},
}

// migrateSenders is the start-up backfill. It adds the derived fields when missing, then fills them for
// records stored before. It is idempotent and does not read Gmail.
func (s *Store) migrateSenders() error {
	have := map[string]bool{}
	rows, err := s.db.Query(`SELECT name FROM pragma_table_info('message_records')`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			rows.Close()
			return err
		}
		have[n] = true
	}
	rows.Close()
	for _, c := range senderColumns {
		if !have[c.name] {
			if _, err := s.db.Exec(`ALTER TABLE message_records ADD COLUMN ` + c.name + ` ` + c.def); err != nil {
				return err
			}
		}
	}
	if _, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS message_records_sender ON message_records (mailbox_id, sender_key, date_ts DESC)`); err != nil {
		return err
	}
	type old struct {
		mailbox            int64
		id, from, date, lb string
	}
	var todo []old
	rows, err = s.db.Query(`SELECT mailbox_id, message_id, sender, date, labels FROM message_records WHERE sender_key = ''`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var o old
		if err := rows.Scan(&o.mailbox, &o.id, &o.from, &o.date, &o.lb); err != nil {
			rows.Close()
			return err
		}
		todo = append(todo, o)
	}
	rows.Close()
	if len(todo) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, o := range todo {
		d := Derive(o.from, o.date, strings.Split(o.lb, ","))
		if _, err := tx.Exec(
			`UPDATE message_records SET sender_key = ?, sender_name = ?, date_ts = ?, unread = ? WHERE mailbox_id = ? AND message_id = ?`,
			d.Key, d.Name, d.Date, boolInt(d.Unread), o.mailbox, o.id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Sort orders sender groups. Every sort is descending, ties by address (D6).
type Sort string

// The three sort fields. SortTotal is the default.
const (
	SortTotal  Sort = "total"
	SortUnread Sort = "unread"
	SortNewest Sort = "newest"
)

// ParseSort maps a page-address value to a sort; anything else is the default (D6).
func ParseSort(v string) Sort {
	switch Sort(v) {
	case SortUnread, SortNewest:
		return Sort(v)
	}
	return SortTotal
}

// SenderGroup is one row of the sender view.
type SenderGroup struct {
	Key     string
	Name    string
	Address string // empty for the unknown sender
	Total   int
	Unread  int
	Newest  int64 // unix seconds, or UnknownDate
}

// MailboxIDs maps an account's mailbox email addresses to their ids.
func (s *Store) MailboxIDs(ctx context.Context, accountID int64) (map[string]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT email, id FROM mailboxes WHERE account_id = ?`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var e string
		var id int64
		if err := rows.Scan(&e, &id); err != nil {
			return nil, err
		}
		out[e] = id
	}
	return out, rows.Err()
}

// SenderGroups reads the account mail set once and returns up to limit groups, plus the number of groups there are.
// The account mail set is every stored email of every mailbox linked to the account, whatever the mailbox status (D4, G9).
func (s *Store) SenderGroups(ctx context.Context, accountID int64, sort Sort, limit int) ([]SenderGroup, int, error) {
	order := `total DESC`
	switch sort {
	case SortUnread:
		order = `unread_n DESC`
	case SortNewest:
		order = `(newest IS NULL), newest DESC`
	}
	q := `WITH mine AS (
			SELECT r.sender_key, r.sender_name, r.date_ts, r.unread, r.mailbox_id, r.message_id
			FROM message_records r JOIN mailboxes m ON m.id = r.mailbox_id WHERE m.account_id = ?1),
		ranked AS (
			SELECT *, ROW_NUMBER() OVER (PARTITION BY sender_key ORDER BY date_ts DESC, mailbox_id, message_id) AS rn FROM mine)
		SELECT sender_key, COALESCE(MAX(CASE WHEN rn = 1 THEN sender_name END), ''), COUNT(*) AS total, SUM(unread) AS unread_n,
			MAX(CASE WHEN date_ts > ?2 THEN date_ts END) AS newest
		FROM ranked GROUP BY sender_key ORDER BY ` + order + `, sender_key LIMIT ?3`
	rows, err := s.db.QueryContext(ctx, q, accountID, UnknownDate, limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []SenderGroup
	for rows.Next() {
		var g SenderGroup
		var newest sql.NullInt64
		if err := rows.Scan(&g.Key, &g.Name, &g.Total, &g.Unread, &newest); err != nil {
			return nil, 0, err
		}
		g.Newest = UnknownDate
		if newest.Valid {
			g.Newest = newest.Int64
		}
		if g.Key != UnknownSenderKey {
			g.Address = g.Key
		}
		out = append(out, g)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	rows.Close()
	var total int
	err = s.db.QueryRowContext(ctx,
		`SELECT COUNT(DISTINCT r.sender_key) FROM message_records r JOIN mailboxes m ON m.id = r.mailbox_id WHERE m.account_id = ?`,
		accountID).Scan(&total)
	return out, total, err
}

// Time returns the group's newest date, and false when no email has a readable date.
func (g SenderGroup) Time() (time.Time, bool) {
	if g.Newest == UnknownDate {
		return time.Time{}, false
	}
	return time.Unix(g.Newest, 0).UTC(), true
}

// SenderSummary is the header of a sender's email list.
type SenderSummary struct {
	Name    string
	Address string
	Total   int
}

// SenderSummary reads the sender's total and newest display name from the same account mail set the groups use (D4).
// An unknown sender has total 0.
func (s *Store) SenderSummary(ctx context.Context, accountID int64, key string) (SenderSummary, error) {
	var sum SenderSummary
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM message_records r JOIN mailboxes m ON m.id = r.mailbox_id
		 WHERE m.account_id = ? AND r.sender_key = ?`, accountID, key).Scan(&sum.Total)
	if err != nil || sum.Total == 0 {
		return sum, err
	}
	err = s.db.QueryRowContext(ctx,
		`SELECT r.sender_name FROM message_records r JOIN mailboxes m ON m.id = r.mailbox_id
		 WHERE m.account_id = ? AND r.sender_key = ? ORDER BY r.date_ts DESC, r.mailbox_id, r.message_id LIMIT 1`,
		accountID, key).Scan(&sum.Name)
	if err != nil {
		return sum, err
	}
	if key != UnknownSenderKey {
		sum.Address = key
	}
	return sum, nil
}

// Email is one row of a sender's email list.
type Email struct {
	MailboxID    int64
	MailboxEmail string // the Gmail account that holds the email
	MessageID    string
	Subject      string
	Date         int64 // unix seconds, or UnknownDate
	Unread       bool
}

// EmailCursor is the continuation point: the last email shown (D8).
type EmailCursor struct {
	Date      int64
	MailboxID int64
	MessageID string
}

// SenderEmails returns up to limit of the sender's emails after the continuation point, newest first,
// unknown dates last, and the next continuation point or nil when no more exist.
func (s *Store) SenderEmails(ctx context.Context, accountID int64, key string, after *EmailCursor, limit int) ([]Email, *EmailCursor, error) {
	q := `SELECT r.mailbox_id, m.email, r.message_id, r.subject, r.date_ts, r.unread
		FROM message_records r JOIN mailboxes m ON m.id = r.mailbox_id
		WHERE m.account_id = ?1 AND r.sender_key = ?2`
	args := []any{accountID, key, limit + 1}
	if after != nil {
		q += ` AND (r.date_ts < ?4 OR (r.date_ts = ?4 AND (r.mailbox_id > ?5 OR (r.mailbox_id = ?5 AND r.message_id > ?6))))`
		args = append(args, after.Date, after.MailboxID, after.MessageID)
	}
	q += ` ORDER BY r.date_ts DESC, r.mailbox_id, r.message_id LIMIT ?3`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var out []Email
	for rows.Next() {
		var e Email
		var unread int
		if err := rows.Scan(&e.MailboxID, &e.MailboxEmail, &e.MessageID, &e.Subject, &e.Date, &unread); err != nil {
			return nil, nil, err
		}
		e.Unread = unread == 1
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	if len(out) <= limit {
		return out, nil, nil
	}
	out = out[:limit]
	last := out[len(out)-1]
	return out, &EmailCursor{Date: last.Date, MailboxID: last.MailboxID, MessageID: last.MessageID}, nil
}

// ErrNoEmail means the account holds no such stored email.
var ErrNoEmail = errors.New("no such email")

// EmailFor reads one stored email of the account, with the Google subject of the mailbox that holds it.
// An email of another account's mailbox gives ErrNoEmail (#43).
func (s *Store) EmailFor(ctx context.Context, accountID, mailboxID int64, messageID string) (Email, string, error) {
	var e Email
	var subject string
	var unread int
	err := s.db.QueryRowContext(ctx,
		`SELECT r.mailbox_id, m.email, m.google_subject, r.message_id, r.subject, r.date_ts, r.unread
		 FROM message_records r JOIN mailboxes m ON m.id = r.mailbox_id
		 WHERE m.account_id = ? AND r.mailbox_id = ? AND r.message_id = ?`,
		accountID, mailboxID, messageID).Scan(&e.MailboxID, &e.MailboxEmail, &subject, &e.MessageID, &e.Subject, &e.Date, &unread)
	if errors.Is(err, sql.ErrNoRows) {
		return Email{}, "", ErrNoEmail
	}
	if err != nil {
		return Email{}, "", err
	}
	e.Unread = unread == 1
	return e, subject, nil
}

// MarkEmailRead records that Gmail took the read mark: the stored email loses its UNREAD label and
// its unread flag, so its sender's unread count drops by one (#43, #44). It is idempotent.
func (s *Store) MarkEmailRead(ctx context.Context, mailboxID int64, messageID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var labels string
	err = tx.QueryRowContext(ctx,
		`SELECT labels FROM message_records WHERE mailbox_id = ? AND message_id = ?`, mailboxID, messageID).Scan(&labels)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNoEmail
	}
	if err != nil {
		return err
	}
	var kept []string
	for _, l := range strings.Split(labels, ",") {
		if l != "" && l != "UNREAD" {
			kept = append(kept, l)
		}
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE message_records SET labels = ?, unread = 0 WHERE mailbox_id = ? AND message_id = ?`,
		strings.Join(kept, ","), mailboxID, messageID); err != nil {
		return err
	}
	return tx.Commit()
}
