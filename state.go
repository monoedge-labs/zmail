package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/lobehub/lobe-mygo/pkg/form"

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

	dir    string  // where privacy.json lives; empty keeps the choice in memory
	dbPath string  // the database's file, "" in memory
	priv   privacy // what is hidden: the window's choice is View, the agent's is Agent
	prefs  prefs   // the window's own choices
	vm     *viewModel
	dl     download

	hidden int  // values hidden in the thread shown, of each message as its own count
	hiding bool // the model has yet to read some of what shows
	rest   int  // messages of the thread not hidden yet: they are once scrolled to

	// A message is hidden when it comes into view, and its hidden copy kept:
	// a message's text never changes, so one is made once for each way of
	// hiding. Until it has one, a message shows as "…", never as its text.
	veils     map[veilKey]veil
	veilBytes int
	want      map[veilKey]bool // in view and without a copy: made before the next frame
	asked     map[veilKey]bool // the model is reading them: asked again once it has read more
	// The mails of the thread the model is reading, since when: one read for
	// long enough to be seen waiting is lit by the beam, and crossed by the
	// wave once it is hidden. waveAt is when that wave began, zero until the
	// frame that first draws it.
	pendAt, waveAt map[int64]time.Time
	v              variant   // how messages show in the frame being built
	late           bool      // this frame held a message without a copy that the frame before did not show
	settleAt       time.Time // when the frame that finds what the last ones showed is due
	noPages        bool      // web views cannot be made here: HTML mail shows as its Markdown
	stamp          string    // the database's files as the last reload found them

	router *ui.Router
	nav    string // the sidebar's choice

	// Mail page.
	query       string
	cat         int // 0 all, then catNames
	rows        []store.Message
	labels      map[int64][]string // the labels of rows, which an agent put on them
	anyLabel    bool               // some mail has a label: the list can be grouped by it
	rowGroup    int                // the group whose rows are being drawn, 0 for the category groups: a mail in two groups needs two keys
	list        ui.ListState
	sel         int // the list's chosen row, -1 for none
	listW       float32
	cur         store.Message // the message shown, valid while hasCur
	hasCur      bool
	thread      []store.Message
	pages       map[pageKey]*mailPage // web views of the thread's messages with HTML, of each way they showed
	face        *agentFace            // the agent's face on the statistics page
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

	add  addState
	link linkAsk
	fb   feedbackState

	sec   mailbox.Secrets // the keychain; nil keeps no secret
	llm   llmSettings     // the model service in use, read once: the keychain is slow
	llmOK bool
}

// feedbackState is the sheet that collects an issue: what the user wrote and,
// once they asked for it, the model's polish of it, which they may edit.
type feedbackState struct {
	open     bool
	text     string
	draft    issueDraft
	polished bool
	busy     bool
	err      string
	set      llmForm
}

// llmForm is the model settings the user is editing in the sheet.
type llmForm struct {
	open bool
	f    *form.Form[llmValues]
	err  string
}

// llmValues are the fields of that form; empty ones are the built-in.
type llmValues struct{ Base, Model, Key string }

// linkAsk is a link the user clicked in mail, waiting for a yes.
type linkAsk struct {
	open bool
	url  string
}

type addState struct {
	open         bool
	provider     int
	step         int // 0 chooses the service, 1 asks for the login
	email        string
	secret       string
	clientID     string
	clientSecret string
	mode         int // Google: 0 is OAuth, 1 is an app password
	shown        int // the mode last drawn, so switching clears the error
	busy         bool
	err          string
	cancel       context.CancelFunc
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
		unread: map[string]int{}, pages: map[pageKey]*mailPage{},
		veils: map[veilKey]veil{}, want: map[veilKey]bool{}, asked: map[veilKey]bool{},
		pendAt: map[int64]time.Time{}, waveAt: map[int64]time.Time{},
	}
	a.vm = &viewModel{dir: a.modelFiles, done: a.refresh, windowed: func() bool { return a.win != nil }, st: st}
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

// poll reloads when another process wrote to the database, as `zmail account
// add` or an agent's command does: its files changed since the last reload.
// Nothing is read or drawn otherwise. Any goroutine may call it.
func (a *app) poll() {
	mygo.RunOnMain(func() {
		if dbStamp(a.dbPath) != a.stamp {
			a.reload()
			a.win.Invalidate()
		}
	})
}

// dbStamp tells the state of a database's files: the size and time of the
// file and of its write-ahead log, where every write lands first.
func dbStamp(path string) string {
	var b strings.Builder
	for _, p := range []string{path, path + "-wal"} {
		if fi, err := os.Stat(p); err == nil {
			fmt.Fprint(&b, fi.Size(), fi.ModTime().UnixNano(), ";")
		}
	}
	return b.String()
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
	a.stamp = dbStamp(a.dbPath)
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
	ids := make([]int64, len(a.rows))
	for i, m := range a.rows {
		ids[i] = m.ID
	}
	a.labels, _ = a.st.Labels(ids)
	counts, _ := a.st.LabelCounts()
	a.anyLabel = len(counts) > 0
	clear(a.asked) // the model may have read more since
	v := a.variant()
	for i := range a.rows {
		a.show(&a.rows[i], false, v, false)
	}

	// Keep the message shown chosen where it now is.
	a.sel = -1
	if a.hasCur {
		for i, m := range a.rows {
			if m.ID == a.cur.ID {
				a.sel = i
			}
		}
		if m, err := a.st.Get(a.cur.ID); err == nil {
			a.show(&m, false, v, true)
			a.cur = m
			a.thread, _ = a.st.Thread(m.ID)
			a.showThread()
		} else {
			a.hasCur, a.thread = false, nil
		}
	}

	a.syncPages()

	a.stats, _ = a.st.Stats(time.Now().AddDate(0, 0, -rangeDays[a.range_]))
	a.syncFace()
}

// variant is a way a message shows: as it is, or with its values hidden by
// the patterns, or by the patterns and the local model.
type variant uint8

const (
	asIs variant = iota
	byPatterns
	byModel
)

// variant is how messages show now.
func (a *app) variant() variant {
	switch {
	case !a.priv.View:
		return asIs
	case a.useModel():
		return byModel
	}
	return byPatterns
}

// veilKey names a hidden copy of a message: of its subject and snippet, which
// a list shows, or of all of it.
type veilKey struct {
	id   int64
	full bool
	v    variant
}

// veil is a message's texts with their values hidden, and how many they were.
type veil struct {
	subject, snippet, body, html string
	hidden                       int
}

// veilBudget is how much text the hidden copies hold before they are dropped.
// ponytail: they are dropped all at once, and made again as they show; drop
// the oldest first if that is ever felt.
const veilBudget = 64 << 20

// hide makes the hidden copy of m. With the local model on, the model reads
// the texts in the background: pending says it has yet to, and the copy is
// then the patterns' work alone, not to be shown.
func (a *app) hide(m store.Message, full bool, v variant) (e veil, pending bool) {
	h := redact.New()
	if v == byModel {
		h.Model = a.vm.find
	}
	t := h.Texts(m.Subject, m.Snippet)
	e.subject, e.snippet = t[0], t[1]
	switch {
	case !full:
	case m.HTML == "":
		e.body = h.Text(m.Body)
	default:
		e.html = h.HTML(m.HTML)
		// The page is what shows. The Markdown body shows only where no web
		// view can be made, and is read only there: the model takes as long
		// over it as over the page.
		if a.win == nil || a.noPages {
			b := redact.New()
			b.Model = h.Model
			e.body = b.Text(m.Body)
			pending = b.Pending
		}
	}
	e.hidden = h.Distinct()
	return e, pending || h.Pending
}

// show puts in m, as the store gave it, what the window may show of it: m as
// it is when nothing is hidden, else its hidden copy. A message without a
// copy gets one at once when now is set, as what is surely in view does; the
// others show as "…" until the view finds them in view (see near). It
// reports whether m shows.
func (a *app) show(m *store.Message, full bool, v variant, now bool) bool {
	if v == asIs {
		return true
	}
	k := veilKey{m.ID, full, v}
	e, ok := a.veils[k]
	if !ok && now && !a.asked[k] {
		var pending bool
		if e, pending = a.hide(*m, full, v); pending {
			a.pend(k)
		} else {
			a.keep(k, e)
			ok = true
		}
	}
	if !ok {
		m.Subject, m.Snippet, m.Body, m.HTML = "…", "", "正在识别敏感信息…", ""
		return false
	}
	m.Subject, m.Snippet, m.Body, m.HTML = e.subject, e.snippet, e.body, e.html
	return true
}

// pend notes that the model is reading the message of k.
func (a *app) pend(k veilKey) {
	a.asked[k] = true
	if _, ok := a.pendAt[k.id]; k.full && !ok {
		a.pendAt[k.id] = time.Now()
	}
}

// keep keeps the hidden copy of k. A mail that was seen waiting for it is
// crossed by the wave as it shows.
func (a *app) keep(k veilKey, e veil) {
	if t, ok := a.pendAt[k.id]; ok && k.full {
		delete(a.pendAt, k.id)
		if time.Since(t) >= beamDelay {
			a.waveAt[k.id] = time.Time{}
		}
	}
	if a.veilBytes > veilBudget {
		clear(a.veils)
		a.veilBytes = 0
	}
	a.veils[k] = e
	a.veilBytes += len(e.subject) + len(e.snippet) + len(e.body) + len(e.html)
}

// showThread shows the thread as the store gave it: its first message at
// once, as it is the one in view when the thread opens, the others as they
// are scrolled to. It counts what is hidden in those that show.
func (a *app) showThread() {
	v := a.variant()
	a.hidden, a.hiding, a.rest = 0, false, 0
	for i := range a.thread {
		k := veilKey{a.thread[i].ID, true, v}
		switch {
		case a.show(&a.thread[i], true, v, i == 0):
			a.hidden += a.veils[k].hidden
		case a.asked[k]:
			a.hiding = true
		default:
			a.rest++
		}
	}
}

// veiled reports whether a message shows as "…" for want of a hidden copy.
func (a *app) veiled(id int64, full bool) bool {
	_, ok := a.veils[veilKey{id, full, a.v}]
	return a.v != asIs && !ok
}

// near is called by the view for a message without a hidden copy, with the
// element that shows it: one in view, or within margin of it, is given its
// copy before the next frame. An element tells whether it showed in the
// frame before, so one new to the window, or scrolled to in one jump, is
// found a frame late: view asks for that frame.
func (a *app) near(c *ui.Context, el ui.Element, id int64, full bool, margin float32) {
	k := veilKey{id, full, a.v}
	switch {
	case a.asked[k]:
	case el.InViewMargin(margin):
		a.want[k] = true
		c.Invalidate()
	default:
		a.late = true
	}
}

// unveil hides the messages the last frame found in view, and shows them.
// Their text is read from the store again: the window held none of it.
func (a *app) unveil() {
	if len(a.want) == 0 {
		return
	}
	v := a.v
	for k := range a.want {
		delete(a.want, k)
		if _, ok := a.veils[k]; ok || k.v != v {
			continue
		}
		m, err := a.st.Get(k.id)
		if err != nil {
			continue
		}
		if e, pending := a.hide(m, k.full, v); pending {
			a.pend(k)
		} else {
			a.keep(k, e)
		}
	}
	for i := range a.rows {
		if e, ok := a.veils[veilKey{a.rows[i].ID, false, v}]; ok {
			a.rows[i].Subject, a.rows[i].Snippet = e.subject, e.snippet
		}
	}
	if a.hasCur {
		if e, ok := a.veils[veilKey{a.cur.ID, false, v}]; ok {
			a.cur.Subject, a.cur.Snippet = e.subject, e.snippet
		}
	}
	a.hidden, a.hiding, a.rest = 0, false, 0
	for i := range a.thread {
		m := &a.thread[i]
		k := veilKey{m.ID, true, v}
		switch e, ok := a.veils[k]; {
		case ok:
			m.Subject, m.Snippet, m.Body, m.HTML = e.subject, e.snippet, e.body, e.html
			a.hidden += e.hidden
		case a.asked[k]:
			a.hiding = true
		default:
			a.rest++
		}
	}
	a.syncPages()
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

// setPrivacy applies and keeps a change of what is hidden. The web views of
// the thread stay, those of the text as it was and of it hidden alike, and
// so do the hidden copies: switching back shows them at once.
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
		a.reload()
	}
}

// openLink follows a link the user clicked in mail. Mail is read-only
// unless the user turned the guard off: the link is held and the window asks
// first. Any goroutine may call it.
func (a *app) openLink(u string) {
	if !external(u) {
		return
	}
	if a.priv.NoLinkGuard {
		openURL(u)
		return
	}
	ask := func() { a.link = linkAsk{open: true, url: u} }
	if a.win == nil { // under test: no window, no other thread
		ask()
		return
	}
	a.win.Update(ask)
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
	clear(a.pendAt)
	clear(a.waveAt)
	a.thread, _ = a.st.Thread(a.cur.ID)
	a.showThread()
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
		a.win.Update(func() { a.finishAdd(err) })
	}()
}

// addGoogleOAuth signs a Google account in through the browser. Workspace
// accounts take this path: the window collects the user's own Desktop client,
// and the refresh token goes to the keychain like a password.
func (a *app) addGoogleOAuth() {
	s := &a.add
	if s.busy {
		return
	}
	email := strings.TrimSpace(s.email)
	id := strings.TrimSpace(s.clientID)
	secret := strings.TrimSpace(s.clientSecret)
	if !strings.Contains(email, "@") || id == "" || secret == "" {
		s.err = "请填写邮箱地址、客户端 ID 和客户端密钥"
		return
	}
	s.busy, s.err = true, ""
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	s.cancel = cancel
	work := func() error {
		defer cancel()
		tok, err := googleLogin(ctx, id, secret, email, openURL)
		if err != nil {
			return err
		}
		acc, err := a.svc.AddAccount("google", email, tok)
		if err != nil {
			return err
		}
		return a.svc.SyncAccount(acc)
	}
	if a.win == nil { // under test: no window, no other thread
		a.finishAdd(work())
		return
	}
	go func() {
		err := work()
		a.win.Update(func() { a.finishAdd(err) })
	}()
}

// finishAdd ends an attempt to add an account, on the main thread.
func (a *app) finishAdd(err error) {
	s := &a.add
	s.busy = false
	s.cancel = nil
	if err == nil {
		s.open = false
		s.email, s.secret, s.clientID, s.clientSecret = "", "", "", ""
		s.err = ""
	} else if s.open && !errors.Is(err, context.Canceled) {
		s.err = err.Error()
		if errors.Is(err, context.DeadlineExceeded) {
			s.err = "登录超时，请重试"
		}
	}
	a.reload()
}

// polishFeedback sends the user's text to the model, and shows the fields
// it makes. Only the user's click gets here.
func (a *app) polishFeedback() {
	f := &a.fb
	if f.busy {
		return
	}
	if strings.TrimSpace(f.text) == "" {
		f.err = "先写几句描述"
		return
	}
	f.busy, f.err = true, ""
	text, cfg := f.text, a.llmNow()
	done := func(d issueDraft, err error) {
		f.busy = false
		if err != nil {
			f.err = err.Error()
			return
		}
		f.draft, f.polished = d, true
	}
	if a.win == nil { // under test: no window, no other thread
		d, err := polish(context.Background(), cfg, text)
		done(d, err)
		return
	}
	go func() {
		d, err := polish(context.Background(), cfg, text)
		a.win.Update(func() { done(d, err) })
	}()
}

// sendFeedback opens the issue page with the polished fields, or with the
// text as it was typed, and closes the sheet.
func (a *app) sendFeedback() {
	f := &a.fb
	d := f.draft
	if !f.polished {
		d = issueDraft{What: strings.TrimSpace(f.text)}
	}
	openIssue(d)
	*f = feedbackState{}
}

// llmNow is the model service in use.
func (a *app) llmNow() llmSettings {
	if !a.llmOK {
		a.llm, a.llmOK = resolveLLM(a.dir, a.sec), true
	}
	return a.llm
}

// toggleLLMForm opens the model settings with what is kept, or closes them.
func (a *app) toggleLLMForm() {
	s := &a.fb.set
	if s.open {
		s.open = false
		return
	}
	c := loadLLMConfig(a.dir)
	*s = llmForm{open: true, f: form.New(llmValues{Base: c.BaseURL, Model: c.Model})}
}

// saveLLM keeps the settings of the form.
func (a *app) saveLLM() {
	s := &a.fb.set
	if err := setLLM(a.dir, a.sec, llmConfig{BaseURL: s.f.Values.Base, Model: s.f.Values.Model}, strings.TrimSpace(s.f.Values.Key)); err != nil {
		s.err = err.Error()
		return
	}
	*s, a.llmOK, a.fb.err = llmForm{}, false, ""
}

// resetLLMForm goes back to the built-in service.
func (a *app) resetLLMForm() {
	if err := resetLLM(a.dir, a.sec); err != nil {
		a.fb.set.err = err.Error()
		return
	}
	a.fb.set, a.llmOK, a.fb.err = llmForm{}, false, ""
}
