package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"

	"github.com/monoedge-labs/zmail/internal/mailbox"
	"github.com/monoedge-labs/zmail/internal/mcpsrv"
	"github.com/monoedge-labs/zmail/internal/store"
)

// app is the state the window shows; its view builds the interface from it.
type app struct {
	win *mygo.Window
	st  *store.Store
	svc *mailbox.Service
	mcp *mcpsrv.Server
	dir string // where the database and the token live

	router *ui.Router
	nav    string // the sidebar's choice

	// Mail page.
	query       string
	cat         int // 0 all, then catNames
	rows        []store.Message
	list        ui.ListState
	sel         int // the list's chosen row, -1 for none
	listW       float32
	cur         store.Message // the message shown, valid while hasCur
	hasCur      bool
	thread      []store.Message
	moveOpen    bool
	newsOpen    bool
	focusSearch bool
	notice      string
	syncing     bool

	// Sidebar.
	accounts []store.Account
	folders  []string
	unread   map[string]int
	total    int

	// Statistics.
	range_ int // index of ranges
	stats  store.Stats

	// Agent page.
	token   string
	tools   []bool
	clients map[string]int
	ops     []store.Op
	mcpErr  string
	copied  string // what was copied last, shown for a moment

	add addState
}

type addState struct {
	open     bool
	provider int
	step     int // 0 chooses the service, 1 asks for the login
	email    string
	secret   string
	busy     bool
	err      string
}

var (
	catNames  = []string{"全部", "人", "通知", "订阅"}
	catValues = []string{"", store.CatPerson, store.CatNotification, store.CatNewsletter}
	rangeDays = []int{7, 30, 90}
)

func newApp(dir string, st *store.Store, svc *mailbox.Service, token, addr string) *app {
	a := &app{
		st: st, svc: svc, dir: dir, token: token,
		router: ui.NewRouter("/mail"), nav: "mail:inbox", sel: -1, listW: 380,
		unread: map[string]int{},
	}
	a.router.Animation = &ui.Snappy
	a.list.Selected = &a.sel
	a.list.Key = func(i int) any { return a.rows[i].ID }
	a.list.Label = func(i int) string { return a.rows[i].Subject }
	a.startMCP(addr)
	for range mcpsrv.Tools {
		a.tools = append(a.tools, true)
	}
	a.reload()
	return a
}

func (a *app) startMCP(addr string) {
	if a.mcp != nil {
		a.mcp.Stop()
	}
	a.mcp = mcpsrv.New(a.svc, a.token, addr)
	for i, on := range a.tools {
		a.mcp.SetEnabled(mcpsrv.Tools[i].Name, on)
	}
	a.mcp.OnCall = func() { a.refresh() }
	if err := a.mcp.Start(); err != nil {
		a.mcpErr = err.Error()
	} else {
		a.mcpErr = ""
	}
}

// refresh reloads the window's data from any goroutine.
func (a *app) refresh() {
	if a.win != nil {
		a.win.Update(a.reload)
	}
}

// async runs work off the main thread, shows its error and reloads.
func (a *app) async(work func() error) {
	done := func(err error) {
		a.notice = ""
		if err != nil {
			a.notice = err.Error()
		}
		a.reload()
	}
	if a.win == nil { // under test: no window, no other thread
		done(work())
		return
	}
	go func() {
		err := work()
		a.win.Update(func() { done(err) })
	}()
}

// reload reads what the pages show from the store. Main thread only.
func (a *app) reload() {
	a.accounts, _ = a.st.Accounts()
	if fs, err := a.st.Folders(""); err == nil {
		a.folders = a.folders[:0]
		for name, role := range fs {
			if role == store.RoleOther {
				a.folders = append(a.folders, name)
			}
		}
		sort.Strings(a.folders)
	}
	a.unread, _ = a.st.UnreadCounts()
	a.total = 0
	for _, n := range a.unread {
		a.total += n
	}

	q := store.Query{Limit: 200, Text: strings.TrimSpace(a.query), Category: catValues[a.cat]}
	switch {
	case a.nav == "mail:sent":
		q.Role = store.RoleSent
	case a.nav == "mail:archive":
		q.Role = store.RoleArchive
	case strings.HasPrefix(a.nav, "folder:"):
		q.Folder = strings.TrimPrefix(a.nav, "folder:")
	case strings.HasPrefix(a.nav, "acct:"):
		q.AccountID, q.Role = strings.TrimPrefix(a.nav, "acct:"), store.RoleInbox
	default:
		q.Role = store.RoleInbox
	}
	a.rows, _ = a.st.Search(q)

	// Keep the message shown chosen where it now is.
	a.sel = -1
	if a.hasCur {
		for i, m := range a.rows {
			if m.ID == a.cur.ID {
				a.sel = i
			}
		}
		if m, err := a.st.Get(a.cur.ID); err == nil {
			a.cur = m
			a.thread, _ = a.st.Thread(m.ID)
		} else {
			a.hasCur, a.thread = false, nil
		}
	}

	a.stats, _ = a.st.Stats(time.Now().AddDate(0, 0, -rangeDays[a.range_]))
	a.clients, _ = a.st.Clients(time.Now().Add(-24 * time.Hour))
	a.ops, _ = a.st.RecentOps(8)
}

// navigate follows the sidebar's choice.
func (a *app) navigate(id string) {
	a.nav = id
	page := "/mail"
	switch id {
	case "page:stats":
		page = "/stats"
	case "page:agent":
		page = "/agent"
	default:
		a.hasCur, a.thread = false, nil
	}
	if a.router.Path() != page {
		a.router.Push(page)
	}
	a.reload()
}

// open shows the message of row i, and reads it.
func (a *app) open(i int) {
	if i < 0 || i >= len(a.rows) {
		return
	}
	a.cur, a.hasCur = a.rows[i], true
	a.thread, _ = a.st.Thread(a.cur.ID)
	if !a.cur.Seen {
		id := a.cur.ID
		a.async(func() error { return a.svc.MarkRead(mailbox.UI, []int64{id}, true) })
	}
}

// threadIDs are the messages of the thread shown that are not yet filed away.
func (a *app) threadIDs(skip ...string) []int64 {
	var ids []int64
next:
	for _, m := range a.thread {
		for _, r := range skip {
			if m.Role == r {
				continue next
			}
		}
		ids = append(ids, m.ID)
	}
	return ids
}

func (a *app) archive() {
	ids := a.threadIDs(store.RoleArchive, store.RoleSent)
	if len(ids) == 0 {
		return
	}
	a.async(func() error { return a.svc.Archive(mailbox.UI, ids) })
}

func (a *app) toggleRead() {
	if !a.hasCur {
		return
	}
	id, seen := a.cur.ID, !a.cur.Seen
	a.async(func() error { return a.svc.MarkRead(mailbox.UI, []int64{id}, seen) })
}

func (a *app) moveThread(folder string) {
	ids := a.threadIDs(store.RoleSent)
	if len(ids) == 0 || folder == "" {
		return
	}
	a.async(func() error { return a.svc.MoveToFolder(mailbox.UI, ids, folder) })
}

func (a *app) syncNow() {
	if a.syncing {
		return
	}
	a.syncing = true
	go func() {
		err := a.svc.SyncAll()
		a.win.Update(func() {
			a.syncing, a.notice = false, ""
			if err != nil {
				a.notice = err.Error()
			}
			a.reload()
		})
	}()
}

func (a *app) addAccount() {
	s := &a.add
	if s.busy {
		return
	}
	p := mailbox.Providers[s.provider]
	email, secret := strings.TrimSpace(s.email), strings.TrimSpace(s.secret)
	if !strings.Contains(email, "@") || secret == "" {
		s.err = "请填写邮箱地址和" + p.Secret
		return
	}
	s.busy, s.err = true, ""
	go func() {
		acc, err := a.svc.AddAccount(p.ID, email, secret)
		if err == nil {
			err = a.svc.SyncAccount(acc)
		}
		a.win.Update(func() {
			s.busy = false
			if err != nil {
				s.err = err.Error()
			} else {
				s.open, s.email, s.secret = false, "", ""
			}
			a.reload()
		})
	}()
}

func (a *app) setTool(i int, on bool) {
	a.tools[i] = on
	a.mcp.SetEnabled(mcpsrv.Tools[i].Name, on)
}

func (a *app) toggleServer() {
	if a.mcp.Running() {
		a.mcp.Stop()
		return
	}
	a.startMCP(a.mcp.Addr)
}

func (a *app) resetToken() {
	tok, err := writeToken(a.dir)
	if err != nil {
		a.mcpErr = err.Error()
		return
	}
	a.token = tok
	a.startMCP(a.mcp.Addr)
}

func (a *app) copy(label, text string) {
	if err := mygo.Clipboard.Write(transfer.TextData(text)); err == nil {
		a.copied = label
	}
}

func (a *app) endpoint() string { return "http://" + a.mcp.Addr + "/mcp" }

func (a *app) claudeCommand() string {
	return fmt.Sprintf(`claude mcp add --transport http zmail %s --header "Authorization: Bearer %s"`, a.endpoint(), a.token)
}

// ---- files

// writeToken makes a new access token for agents and keeps it in the
// config directory, readable by the user alone.
func writeToken(dir string) (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	tok := "zm_" + hex.EncodeToString(b)
	return tok, os.WriteFile(filepath.Join(dir, "token"), []byte(tok), 0o600)
}

// loadToken reads the token, making one on first run.
func loadToken(dir string) (string, error) {
	if b, err := os.ReadFile(filepath.Join(dir, "token")); err == nil && strings.HasPrefix(string(b), "zm_") {
		return strings.TrimSpace(string(b)), nil
	}
	return writeToken(dir)
}
