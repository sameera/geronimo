package store

import (
	"context"
	"database/sql"
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

// SortTotal is the default sort.
const SortTotal Sort = "total"

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
