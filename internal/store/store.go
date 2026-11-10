// Package store keeps accounts, mail and the log of what was done to it, in
// one SQLite file. Mail is a local cache of the servers' mailboxes: the
// service writes to the server first and then to this store.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure Go driver: MyGo builds without cgo
)

// Roles of a folder. Every message carries the role of the folder it is in.
const (
	RoleInbox   = "inbox"
	RoleSent    = "sent"
	RoleArchive = "archive"
	RoleOther   = "other"
)

// Categories the inbox groups messages by.
const (
	CatPerson       = "person"
	CatNotification = "notification"
	CatNewsletter   = "newsletter"
)

// Who did something to a message.
const (
	ActorMe    = "me"
	ActorAgent = "agent"
)

type Account struct {
	ID       string // stable, e.g. "gmail:me@gmail.com"
	Provider string // "google", "aliyun", "qq"
	Email    string
	Host     string // IMAP host:port
}

type Message struct {
	ID        int64
	AccountID string
	Folder    string
	Role      string
	UID       uint32
	ThreadKey string
	FromName  string
	FromAddr  string
	Subject   string
	Snippet   string
	Body      string
	Date      time.Time
	Seen      bool
	Category  string
	HandledBy string // "", ActorMe or ActorAgent: who first read or filed it
}

// Query selects messages. Zero fields do not filter.
type Query struct {
	Text      string // matches subject, sender and body
	AccountID string
	Folder    string
	Role      string
	Category  string
	Unread    bool
	Since     time.Time
	Limit     int
}

type DayCount struct {
	Day       string // 2006-01-02, local time
	Agent     int
	Me        int
	Untouched int
}

type Stats struct {
	Received                int
	Sent                    int
	Unread                  int
	ByAccount               map[string]int
	TopSenders              []SenderCount
	Days                    []DayCount
	AgentOps                map[string]int // tool name -> calls
	AgentHandled, MeHandled int
}

type SenderCount struct {
	Name  string
	Addr  string
	Count int
}

type Op struct {
	Time   time.Time
	Actor  string
	Client string
	Tool   string
	Detail string
	OK     bool
}

type Store struct{ db *sql.DB }

const schema = `
CREATE TABLE IF NOT EXISTS accounts (
	id TEXT PRIMARY KEY, provider TEXT NOT NULL, email TEXT NOT NULL, host TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS folders (
	account_id TEXT NOT NULL, name TEXT NOT NULL, role TEXT NOT NULL,
	PRIMARY KEY (account_id, name)
);
CREATE TABLE IF NOT EXISTS messages (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	account_id TEXT NOT NULL, folder TEXT NOT NULL, role TEXT NOT NULL, uid INTEGER NOT NULL,
	thread_key TEXT NOT NULL, from_name TEXT NOT NULL, from_addr TEXT NOT NULL,
	subject TEXT NOT NULL, snippet TEXT NOT NULL, body TEXT NOT NULL,
	date INTEGER NOT NULL, seen INTEGER NOT NULL, category TEXT NOT NULL,
	handled_by TEXT NOT NULL DEFAULT '',
	UNIQUE (account_id, folder, uid)
);
CREATE INDEX IF NOT EXISTS messages_date ON messages (date DESC);
CREATE INDEX IF NOT EXISTS messages_thread ON messages (account_id, thread_key);
CREATE TABLE IF NOT EXISTS ops (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	ts INTEGER NOT NULL, actor TEXT NOT NULL, client TEXT NOT NULL,
	tool TEXT NOT NULL, detail TEXT NOT NULL, ok INTEGER NOT NULL
);
`

// Open opens (and creates) the database at path; ":memory:" keeps it in memory.
func Open(path string) (*Store, error) {
	dsn := path
	if path != ":memory:" {
		dsn = "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if path == ":memory:" {
		db.SetMaxOpenConns(1) // every connection would be its own database
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	// Version 2 keeps bodies as Markdown. The messages are a copy of the
	// server's, so older ones are dropped and synced again.
	var version int
	db.QueryRow(`PRAGMA user_version`).Scan(&version)
	if version < 2 {
		if _, err := db.Exec(`DELETE FROM messages; PRAGMA user_version = 2`); err != nil {
			db.Close()
			return nil, err
		}
	}
	return &Store{db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) UpsertAccount(a Account) error {
	_, err := s.db.Exec(`INSERT INTO accounts (id, provider, email, host) VALUES (?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET provider=excluded.provider, email=excluded.email, host=excluded.host`,
		a.ID, a.Provider, a.Email, a.Host)
	return err
}

func (s *Store) Accounts() ([]Account, error) {
	rows, err := s.db.Query(`SELECT id, provider, email, host FROM accounts ORDER BY email`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Account
	for rows.Next() {
		var a Account
		if err := rows.Scan(&a.ID, &a.Provider, &a.Email, &a.Host); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) DeleteAccount(id string) error {
	for _, q := range []string{
		`DELETE FROM messages WHERE account_id = ?`,
		`DELETE FROM folders WHERE account_id = ?`,
		`DELETE FROM accounts WHERE id = ?`,
	} {
		if _, err := s.db.Exec(q, id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) SetFolder(accountID, name, role string) error {
	_, err := s.db.Exec(`INSERT INTO folders (account_id, name, role) VALUES (?,?,?)
		ON CONFLICT(account_id, name) DO UPDATE SET role=excluded.role`, accountID, name, role)
	return err
}

// Folders lists an account's folders, or every account's when accountID is "".
func (s *Store) Folders(accountID string) (map[string]string, error) {
	q, args := `SELECT name, role FROM folders`, []any{}
	if accountID != "" {
		q, args = q+` WHERE account_id = ?`, append(args, accountID)
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var n, r string
		if err := rows.Scan(&n, &r); err != nil {
			return nil, err
		}
		out[n] = r
	}
	return out, rows.Err()
}

// FolderRole returns the role of a folder, RoleOther if unknown.
func (s *Store) FolderRole(accountID, name string) string {
	var r string
	if err := s.db.QueryRow(`SELECT role FROM folders WHERE account_id=? AND name=?`, accountID, name).Scan(&r); err != nil {
		return RoleOther
	}
	return r
}

// LastUID is the highest UID kept for a folder, 0 for none.
func (s *Store) LastUID(accountID, folder string) uint32 {
	var u sql.NullInt64
	s.db.QueryRow(`SELECT MAX(uid) FROM messages WHERE account_id=? AND folder=?`, accountID, folder).Scan(&u)
	return uint32(u.Int64)
}

// UpsertMessage adds a message, or refreshes its flags if it is known. A
// known message keeps who handled it.
func (s *Store) UpsertMessage(m Message) error {
	_, err := s.db.Exec(`INSERT INTO messages
		(account_id, folder, role, uid, thread_key, from_name, from_addr, subject, snippet, body, date, seen, category)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(account_id, folder, uid) DO UPDATE SET seen=excluded.seen, role=excluded.role`,
		m.AccountID, m.Folder, m.Role, m.UID, m.ThreadKey, m.FromName, m.FromAddr, m.Subject,
		m.Snippet, m.Body, m.Date.Unix(), b2i(m.Seen), m.Category)
	return err
}

const cols = `id, account_id, folder, role, uid, thread_key, from_name, from_addr, subject, snippet, body, date, seen, category, handled_by`

func scan(r interface{ Scan(...any) error }) (Message, error) {
	var m Message
	var date int64
	var seen int
	err := r.Scan(&m.ID, &m.AccountID, &m.Folder, &m.Role, &m.UID, &m.ThreadKey, &m.FromName, &m.FromAddr,
		&m.Subject, &m.Snippet, &m.Body, &date, &seen, &m.Category, &m.HandledBy)
	m.Date, m.Seen = time.Unix(date, 0), seen != 0
	return m, err
}

func (s *Store) Get(id int64) (Message, error) {
	return scan(s.db.QueryRow(`SELECT `+cols+` FROM messages WHERE id = ?`, id))
}

// Search returns messages newest first. The body is left out unless the
// caller reads one message with Get.
func (s *Store) Search(q Query) ([]Message, error) {
	var where []string
	var args []any
	add := func(cond string, a ...any) { where, args = append(where, cond), append(args, a...) }
	if q.Text != "" {
		like := "%" + escapeLike(q.Text) + "%"
		add(`(subject LIKE ? ESCAPE '\' OR from_name LIKE ? ESCAPE '\' OR from_addr LIKE ? ESCAPE '\' OR body LIKE ? ESCAPE '\')`,
			like, like, like, like)
	}
	if q.AccountID != "" {
		add(`account_id = ?`, q.AccountID)
	}
	if q.Folder != "" {
		add(`folder = ?`, q.Folder)
	}
	if q.Role != "" {
		add(`role = ?`, q.Role)
	}
	if q.Category != "" {
		add(`category = ?`, q.Category)
	}
	if q.Unread {
		add(`seen = 0`)
	}
	if !q.Since.IsZero() {
		add(`date >= ?`, q.Since.Unix())
	}
	sqlText := `SELECT ` + cols + ` FROM messages`
	if len(where) > 0 {
		sqlText += ` WHERE ` + strings.Join(where, ` AND `)
	}
	limit := q.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	sqlText += fmt.Sprintf(` ORDER BY date DESC LIMIT %d`, limit)
	rows, err := s.db.Query(sqlText, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		m, err := scan(rows)
		if err != nil {
			return nil, err
		}
		m.Body = ""
		out = append(out, m)
	}
	return out, rows.Err()
}

// Thread returns the messages sharing m's account and thread key, oldest first.
func (s *Store) Thread(id int64) ([]Message, error) {
	m, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT `+cols+` FROM messages WHERE account_id=? AND thread_key=? ORDER BY date`, m.AccountID, m.ThreadKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		t, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// MarkSeen records a read flag the server accepted, and who set it.
func (s *Store) MarkSeen(ids []int64, seen bool, actor string) error {
	return s.each(ids, func(id int64) error {
		_, err := s.db.Exec(`UPDATE messages SET seen=?, handled_by = CASE WHEN handled_by='' THEN ? ELSE handled_by END WHERE id=?`,
			b2i(seen), actor, id)
		return err
	})
}

// UnreadCounts returns the unread messages of each account's inbox.
func (s *Store) UnreadCounts() (map[string]int, error) {
	rows, err := s.db.Query(`SELECT account_id, COUNT(*) FROM messages WHERE role=? AND seen=0 GROUP BY account_id`, RoleInbox)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

// DeleteMessage forgets a message locally. The server's copy is untouched.
func (s *Store) DeleteMessage(id int64) error {
	_, err := s.db.Exec(`DELETE FROM messages WHERE id=?`, id)
	return err
}

// SetSeenByUID applies read flags another client set on the server. It
// does not count as handling the message.
func (s *Store) SetSeenByUID(accountID, folder string, seen map[uint32]bool) error {
	for uid, v := range seen {
		if _, err := s.db.Exec(`UPDATE messages SET seen=? WHERE account_id=? AND folder=? AND uid=?`,
			b2i(v), accountID, folder, uid); err != nil {
			return err
		}
	}
	return nil
}

// Move records that the server moved a message to a folder. Its UID there
// is new, so the caller passes it; 0 keeps the old one and the next sync
// reconciles.
func (s *Store) Move(id int64, folder, role string, newUID uint32, actor string) error {
	_, err := s.db.Exec(`UPDATE messages SET folder=?, role=?, uid = CASE WHEN ?>0 THEN ? ELSE uid END,
		handled_by = CASE WHEN handled_by='' THEN ? ELSE handled_by END WHERE id=?`,
		folder, role, newUID, newUID, actor, id)
	return err
}

func (s *Store) each(ids []int64, f func(int64) error) error {
	for _, id := range ids {
		if err := f(id); err != nil {
			return err
		}
	}
	return nil
}

// LogOp records a call: a tool an agent used, or an action of the user.
func (s *Store) LogOp(o Op) error {
	if o.Time.IsZero() {
		o.Time = time.Now()
	}
	_, err := s.db.Exec(`INSERT INTO ops (ts, actor, client, tool, detail, ok) VALUES (?,?,?,?,?,?)`,
		o.Time.Unix(), o.Actor, o.Client, o.Tool, o.Detail, b2i(o.OK))
	return err
}

func (s *Store) RecentOps(n int) ([]Op, error) {
	rows, err := s.db.Query(`SELECT ts, actor, client, tool, detail, ok FROM ops ORDER BY id DESC LIMIT ?`, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Op
	for rows.Next() {
		var o Op
		var ts int64
		var ok int
		if err := rows.Scan(&ts, &o.Actor, &o.Client, &o.Tool, &o.Detail, &ok); err != nil {
			return nil, err
		}
		o.Time, o.OK = time.Unix(ts, 0), ok != 0
		out = append(out, o)
	}
	return out, rows.Err()
}

// Clients lists the agents that called since a time, with their call counts.
func (s *Store) Clients(since time.Time) (map[string]int, error) {
	rows, err := s.db.Query(`SELECT client, COUNT(*) FROM ops WHERE actor=? AND ts>=? GROUP BY client`, ActorAgent, since.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var c string
		var n int
		if err := rows.Scan(&c, &n); err != nil {
			return nil, err
		}
		out[c] = n
	}
	return out, rows.Err()
}

// Stats summarises the mail received since a time, grouped as the statistics page shows it.
func (s *Store) Stats(since time.Time) (Stats, error) {
	st := Stats{ByAccount: map[string]int{}, AgentOps: map[string]int{}}
	one := func(q string, args ...any) int {
		var n int
		s.db.QueryRow(q, args...).Scan(&n)
		return n
	}
	st.Received = one(`SELECT COUNT(*) FROM messages WHERE role!=? AND date>=?`, RoleSent, since.Unix())
	st.Sent = one(`SELECT COUNT(*) FROM messages WHERE role=? AND date>=?`, RoleSent, since.Unix())
	st.Unread = one(`SELECT COUNT(*) FROM messages WHERE role=? AND seen=0`, RoleInbox)
	st.AgentHandled = one(`SELECT COUNT(*) FROM messages WHERE role!=? AND date>=? AND handled_by=?`, RoleSent, since.Unix(), ActorAgent)
	st.MeHandled = one(`SELECT COUNT(*) FROM messages WHERE role!=? AND date>=? AND handled_by=?`, RoleSent, since.Unix(), ActorMe)

	rows, err := s.db.Query(`SELECT a.email, COUNT(*) FROM messages m JOIN accounts a ON a.id=m.account_id
		WHERE m.role!=? AND m.date>=? GROUP BY a.email ORDER BY 2 DESC`, RoleSent, since.Unix())
	if err != nil {
		return st, err
	}
	for rows.Next() {
		var e string
		var n int
		rows.Scan(&e, &n)
		st.ByAccount[e] = n
	}
	rows.Close()

	rows, err = s.db.Query(`SELECT from_name, from_addr, COUNT(*) FROM messages WHERE role!=? AND date>=?
		GROUP BY from_addr ORDER BY 3 DESC LIMIT 5`, RoleSent, since.Unix())
	if err != nil {
		return st, err
	}
	for rows.Next() {
		var c SenderCount
		rows.Scan(&c.Name, &c.Addr, &c.Count)
		st.TopSenders = append(st.TopSenders, c)
	}
	rows.Close()

	rows, err = s.db.Query(`SELECT strftime('%Y-%m-%d', date, 'unixepoch', 'localtime') d, handled_by, COUNT(*)
		FROM messages WHERE role!=? AND date>=? GROUP BY d, handled_by ORDER BY d`, RoleSent, since.Unix())
	if err != nil {
		return st, err
	}
	idx := map[string]int{}
	for rows.Next() {
		var d, by string
		var n int
		rows.Scan(&d, &by, &n)
		i, ok := idx[d]
		if !ok {
			i = len(st.Days)
			idx[d] = i
			st.Days = append(st.Days, DayCount{Day: d})
		}
		switch by {
		case ActorAgent:
			st.Days[i].Agent += n
		case ActorMe:
			st.Days[i].Me += n
		default:
			st.Days[i].Untouched += n
		}
	}
	rows.Close()

	rows, err = s.db.Query(`SELECT tool, COUNT(*) FROM ops WHERE actor=? AND ts>=? AND ok=1 GROUP BY tool`, ActorAgent, since.Unix())
	if err != nil {
		return st, err
	}
	for rows.Next() {
		var t string
		var n int
		rows.Scan(&t, &n)
		st.AgentOps[t] = n
	}
	rows.Close()
	return st, nil
}

// ErrNotFound is returned when an id names nothing.
var ErrNotFound = errors.New("not found")

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
