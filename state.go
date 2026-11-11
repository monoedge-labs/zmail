package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/monoedge-labs/zmail/internal/guard"
	"github.com/monoedge-labs/zmail/internal/mailbox"
	"github.com/monoedge-labs/zmail/internal/redact"
	"github.com/monoedge-labs/zmail/internal/store"
)

// app is the state the window shows; its view builds the interface from it.
type app struct {
	win *mygo.Window
	st  *store.Store
	svc *mailbox.Service

	dir  string  // where privacy.json lives; empty keeps the choice in memory
	priv privacy // what is hidden: the window's choice is View, the agent's is Agent
	vm   *viewModel
	dl   download

	hidden int  // values hidden in the thread shown, of each message as its own count
	hiding bool // the model has yet to read some of it

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
	pages       map[int64]*mailPage // web views of the thread's messages with HTML
	face        *agentFace          // the agent's face on the statistics page
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

func newApp(st *store.Store, svc *mailbox.Service) *app {
	a := &app{
		st: st, svc: svc,
		router: ui.NewRouter("/mail"), nav: "mail:inbox", sel: -1, listW: 380,
		unread: map[string]int{}, pages: map[int64]*mailPage{},
	}
	a.vm = &viewModel{dir: a.modelFiles, done: a.refresh, windowed: func() bool { return a.win != nil }}
	a.router.Animation = &ui.Snappy
	a.list.Selected = &a.sel
	a.list.Key = func(i int) any { return a.rows[i].ID }
	a.list.Label = func(i int) string { return a.rows[i].Subject }
	a.reload()
	return a
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
	case a.nav == "mail:junk":
		q.Role = store.RoleJunk
	case strings.HasPrefix(a.nav, "folder:"):
		q.Folder = strings.TrimPrefix(a.nav, "folder:")
	case strings.HasPrefix(a.nav, "acct:"):
		q.AccountID, q.Role = strings.TrimPrefix(a.nav, "acct:"), store.RoleInbox
	default:
		q.Role = store.RoleInbox
	}
	a.rows, _ = a.st.Search(q)
	a.maskAll(a.rows)

	// Keep the message shown chosen where it now is.
	a.sel = -1
	if a.hasCur {
		for i, m := range a.rows {
			if m.ID == a.cur.ID {
				a.sel = i
			}
		}
		if m, err := a.st.Get(a.cur.ID); err == nil {
			a.mask(&m)
			a.cur = m
			a.thread, _ = a.st.Thread(m.ID)
			a.hidden, a.hiding = a.maskAll(a.thread)
		} else {
			a.hasCur, a.thread = false, nil
		}
	}

	a.syncPages()

	a.stats, _ = a.st.Stats(time.Now().AddDate(0, 0, -rangeDays[a.range_]))
	a.syncFace()
}

// mask hides the private values in the texts of m, when the window is set
// to. A message is masked on its way from the store, so every page, the list
// and the web views alike, shows it hidden. With the local model on, the
// model reads the texts in the background: until it has, a message shows as
// "…" or a notice, never as the text.
func (a *app) mask(m *store.Message) (hidden int, pending bool) {
	if !a.priv.View {
		return 0, false
	}
	h := redact.New()
	if a.useModel() {
		h.Model = a.vm.find
	}
	t := h.Texts(m.Subject, m.Snippet)
	m.Subject, m.Snippet = t[0], t[1]
	if m.HTML != "" {
		// The page is what is shown, and read first (the model reads the
		// newest ask first); the Markdown body shows only where the page
		// cannot be made, and has its own reading, which waits for none.
		b := redact.New()
		b.Model = h.Model
		m.Body = b.Text(m.Body)
		if b.Pending {
			m.Body = "正在识别敏感信息…"
		}
		m.HTML = h.HTML(m.HTML)
	} else {
		m.Body = h.Text(m.Body)
	}
	if !h.Pending {
		return h.Distinct(), false
	}
	m.Subject, m.Snippet, m.Body, m.HTML = "…", "", "正在识别敏感信息…", ""
	return 0, true
}

// useModel says whether the local model reads what is hidden now: it is on,
// its files are here and it has not failed. Until then the patterns hide
// what they can, and maskNote says what is left.
func (a *app) useModel() bool {
	return a.priv.model() && guard.Installed(a.modelFiles()) && a.vm.failure() == ""
}

// maskNote says how far hiding has got when it falls short of the model's, and
// offers what would mend it ("" for none).
func (a *app) maskNote() (note, act string) {
	switch {
	case a.useModel():
		return "", ""
	case !a.priv.model():
		return "本地识别模型已关闭：姓名和地址没有隐藏", "开启"
	case a.dl.busy:
		pct := int64(0)
		if a.dl.total > 0 {
			pct = a.dl.done * 100 / a.dl.total
		}
		return fmt.Sprintf("正在下载识别模型 %d%%，完成后会隐藏姓名和地址", pct), ""
	case a.dl.err != "":
		return "识别模型下载失败：" + a.dl.err, "重试"
	case a.vm.failure() != "":
		return a.vm.failure(), "重试"
	}
	return "识别模型还没有下载：姓名和地址没有隐藏", "下载"
}

// maskAll masks messages, newest asked last, and counts what it hid.
func (a *app) maskAll(ms []store.Message) (hidden int, pending bool) {
	for i := len(ms) - 1; i >= 0; i-- {
		n, w := a.mask(&ms[i])
		hidden, pending = hidden+n, pending || w
	}
	return hidden, pending
}

// setPrivacy applies and keeps a change of what is hidden. The web views are
// made again: their pages hold the old text.
func (a *app) setPrivacy(p privacy) {
	if p == a.priv {
		return
	}
	viewChanged := p.View != a.priv.View || p.ModelOff != a.priv.ModelOff
	a.priv = p
	a.ensureModel()
	if a.dir != "" {
		if err := p.save(a.dir); err != nil {
			a.notice = err.Error()
		}
	}
	if viewChanged {
		for id, pg := range a.pages {
			pg.view.Destroy()
			delete(a.pages, id)
		}
		a.reload()
	}
}

// navigate follows the sidebar's choice.
func (a *app) navigate(id string) {
	a.nav = id
	page := "/mail"
	switch id {
	case "page:stats":
		page = "/stats"
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
	a.hidden, a.hiding = a.maskAll(a.thread)
	a.syncPages()
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
