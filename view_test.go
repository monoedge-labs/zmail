package main

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/monoedge-labs/zmail/internal/mailbox"
	"github.com/monoedge-labs/zmail/internal/store"
)

func testApp(t *testing.T) *app {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	svc := demoService(st)
	return newApp(st, svc)
}

// shot saves what the window shows in $ZMAIL_SHOTS, when it is set.
func shot(t *testing.T, tt *ui.Tester, name string) {
	dir := os.Getenv("ZMAIL_SHOTS")
	if dir == "" {
		return
	}
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, tt.Image()); err != nil {
		t.Fatal(err)
	}
}

func TestPages(t *testing.T) {
	a := testApp(t)
	tt := ui.NewTester(a.view, 1360, 900)
	if !tt.HasText("设计评审改到周四 15:00") || !tt.HasText("收件箱") {
		t.Fatalf("inbox not shown: %q", tt.Texts())
	}
	shot(t, tt, "1-inbox")

	if err := tt.Click("设计评审改到周四 15:00"); err != nil {
		t.Fatal(err)
	}
	if !a.hasCur || !tt.HasText("议程在这里") {
		t.Fatalf("message not opened: %q", tt.Texts())
	}
	tt.Advance(400e6)
	shot(t, tt, "2-reader")

	if err := tt.Click("归档（⌘E）"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(400e6)
	if tt.HasText("设计评审改到周四 15:00") && a.nav == "mail:inbox" {
		// the subject may still show in the reader's title; the list must not hold it
		for _, m := range a.rows {
			if m.Subject == "设计评审改到周四 15:00" {
				t.Error("archived message is still in the inbox")
			}
		}
	}

	if err := tt.Click("统计"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(600e6)
	if !tt.HasText("每日处理的邮件") {
		t.Fatalf("stats not shown: %q", tt.Texts())
	}
	shot(t, tt, "3-stats")

	for _, text := range tt.Texts() {
		if strings.Contains(text, "Agent 接入") || strings.Contains(strings.ToLower(text), "mcp") {
			t.Errorf("the window still shows %q", text)
		}
	}

	if err := tt.Click("添加账户"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(300e6)
	if !tt.HasText("选择邮箱服务商，邮件同步后保存在本机。") {
		t.Fatalf("add dialog not shown: %q", tt.Texts())
	}
	shot(t, tt, "5-add")

	if err := tt.Click("QQ 邮箱"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(300e6)
	if !tt.HasText("登录 QQ 邮箱") || !tt.HasText("连接") {
		t.Fatalf("sign-in step not shown: %q", tt.Texts())
	}
	shot(t, tt, "5b-signin")
	if err := tt.Click("返回"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("关闭"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(400e6)
	if a.add.open {
		t.Error("the close button must close the sheet")
	}
}

func TestDark(t *testing.T) {
	a := testApp(t)
	tt := ui.NewTester(a.view, 1360, 900)
	tt.SetDark(true)
	if err := tt.Click("设计评审改到周四 15:00"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(400e6)
	shot(t, tt, "6-dark")
}

// A provider's sign-in form leads to that provider's own help page, and
// only opens it when asked: the address and the password stay in the form.
func TestProviderHelp(t *testing.T) {
	var opened []string
	open := openURL
	openURL = func(u string) { opened = append(opened, u) }
	t.Cleanup(func() { openURL = open })

	for _, dark := range []bool{false, true} {
		a := testApp(t)
		tt := ui.NewTester(a.view, 1360, 900)
		tt.SetDark(dark)
		for i, pr := range mailbox.Providers {
			opened = nil
			if err := tt.Click("添加账户"); err != nil {
				t.Fatal(err)
			}
			tt.Advance(300e6)
			if err := tt.Click(services[i].name); err != nil {
				t.Fatal(err)
			}
			tt.Advance(300e6)
			link := "如何获取" + pr.Secret
			if !tt.HasText(link) || !tt.HasText(pr.HelpNote+"（"+helpHost(pr)+"）") {
				t.Fatalf("%s: no help in the form: %q", pr.ID, tt.Texts())
			}
			if len(opened) != 0 {
				t.Fatalf("%s: showing the form opened %v", pr.ID, opened)
			}
			a.add.email, a.add.secret = "me@example.com", "s3cret"
			if err := tt.Click(link); err != nil {
				t.Fatal(err)
			}
			if len(opened) != 1 || opened[0] != pr.Help {
				t.Fatalf("%s: opened %v, want [%s]", pr.ID, opened, pr.Help)
			}
			shot(t, tt, fmt.Sprintf("signin-%s-dark%v", pr.ID, dark))
			if err := tt.Click("关闭"); err != nil {
				t.Fatal(err)
			}
			tt.Advance(400e6)
		}
	}
}

func TestSearchAndAccounts(t *testing.T) {
	a := testApp(t)
	tt := ui.NewTester(a.view, 1360, 900)
	tt.Key(ui.Cmd, ui.KeyF)
	tt.Type("设计评审")
	tt.Advance(300e6)
	if len(a.rows) == 0 || !tt.HasText("设计评审改到周四 15:00") {
		t.Fatalf("search found nothing: %q", tt.Texts())
	}
	shot(t, tt, "8-search")

	if err := tt.Click("阿里邮箱"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(300e6)
	if a.nav != "acct:aliyun:me@aliyun.com" {
		t.Fatalf("account not chosen: %q", a.nav)
	}
	shot(t, tt, "9-account")
}

// The junk folder is a sidebar page of its own, and mail in it stays out of the inbox.
func TestJunkPage(t *testing.T) {
	a := testApp(t)
	acc := a.accounts[0].ID
	a.st.SetFolder(acc, "Junk", store.RoleJunk)
	if err := a.st.UpsertMessage(store.Message{AccountID: acc, Folder: "Junk", Role: store.RoleJunk, UID: 1, ThreadKey: "prize",
		FromName: "Promo", FromAddr: "p@spam.biz", Subject: "你中奖了", Body: "点击领取", Date: time.Now()}); err != nil {
		t.Fatal(err)
	}
	a.reload()
	tt := ui.NewTester(a.view, 1360, 900)
	if tt.HasText("你中奖了") {
		t.Fatal("junk shows in the inbox")
	}
	if err := tt.Click("垃圾邮件"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(300e6)
	if !tt.HasText("你中奖了") {
		t.Fatalf("junk page empty: %q", tt.Texts())
	}
	shot(t, tt, "10-junk")
	if err := tt.Click("你中奖了"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(300e6)
	if !a.hasCur || len(a.thread) != 1 {
		t.Fatalf("junk mail not opened: %+v", a.thread)
	}
}

// Mails of one thread are told apart by a line between them.
func TestThreadRule(t *testing.T) {
	a := testApp(t)
	tt := ui.NewTester(a.view, 1360, 900)
	if err := tt.Click("Re: 图标稿"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(400e6)
	if len(a.thread) < 2 {
		t.Fatalf("thread = %d messages", len(a.thread))
	}
	shot(t, tt, "11-thread")
}

// The shield in the list's header hides private values in the list and the
// reader, and keeps the choice.
func TestHidePrivate(t *testing.T) {
	a := testApp(t)
	a.dir = t.TempDir()
	acc := a.accounts[0].ID
	if err := a.st.UpsertMessage(store.Message{AccountID: acc, Folder: "INBOX", Role: store.RoleInbox, UID: 900, ThreadKey: "code",
		FromName: "Bank", FromAddr: "bank@x.com", Subject: "验证码 481516", Snippet: "致电 13800138000", Body: "写信给 lin@acme.com\n\n你的验证码：481516",
		HTML: `<p>你的验证码：481516，联系 <a href="mailto:lin@acme.com">lin@acme.com</a></p>`, Date: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	a.reload()
	tt := ui.NewTester(a.view, 1360, 900)
	if !tt.HasText("验证码 481516") {
		t.Fatalf("the code is hidden by default: %q", tt.Texts())
	}
	if err := tt.Click("打码：已关闭"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(300e6)
	if tt.HasText("验证码 481516") || !tt.HasText("验证码 验证码 1") {
		t.Fatalf("subject not hidden, or not shown as a chip: %q", tt.Texts())
	}
	if !a.priv.View || !loadPrivacy(a.dir).View {
		t.Errorf("choice not kept: %+v %+v", a.priv, loadPrivacy(a.dir))
	}
	if err := tt.Click("验证码 验证码 1"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(300e6)
	if strings.Contains(a.cur.Body+a.cur.HTML+a.cur.Snippet, "481516") || strings.Contains(a.cur.Body+a.cur.HTML, "lin@acme.com") ||
		!strings.Contains(a.cur.HTML, "mailto:‹EMAIL_1›") {
		t.Errorf("message not hidden: %+v", a.cur)
	}
	// the page's own markup, which the web view loads, is what is hidden
	if h := a.thread[len(a.thread)-1].HTML; strings.Contains(h, "481516") || strings.Contains(h, "lin@acme.com") || !strings.Contains(h, `data-k="CODE"`) {
		t.Errorf("thread html not hidden: %s", h)
	}
	// the store keeps the real text
	if m, _ := a.st.Get(a.cur.ID); !strings.Contains(m.Body, "481516") {
		t.Error("masking must not touch the store")
	}
	// the banner says that it is on, and how many values it hid
	banner := false
	for _, s := range tt.Texts() {
		banner = banner || strings.HasPrefix(s, "已隐藏 3 项敏感信息") // the code, the address and the number in the snippet
	}
	if !banner {
		t.Errorf("no banner: %q", tt.Texts())
	}
	shot(t, tt, "12-hidden")
	if err := tt.Click("显示原文"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(300e6)
	if a.priv.View || !strings.Contains(a.cur.Body, "481516") {
		t.Errorf("not shown again: %+v", a.priv)
	}
}

// What the agent may read is chosen on the statistics page and kept in the config directory.
func TestAgentPrivacyKept(t *testing.T) {
	a := testApp(t)
	a.dir = t.TempDir()
	a.setPrivacy(privacy{Agent: true})
	if got := loadPrivacy(a.dir); !got.Agent || got.View {
		t.Errorf("kept %+v", got)
	}
	tt := ui.NewTester(a.view, 1360, 900)
	if err := tt.Click("统计"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(600e6)
	if !tt.HasText("用本地模型识别姓名和地址") || !tt.HasText("下载识别模型（约 38 MB）") {
		t.Errorf("the model's switch and download are not shown: %q", tt.Texts())
	}
	shot(t, tt, "14-stats-privacy")
}

// With the model on, the opened message is read by it and shown hidden, names
// and addresses too; without the model's files the message says so instead.
func TestHideWithModel(t *testing.T) {
	a := testApp(t)
	a.dir = t.TempDir()
	acc := a.accounts[0].ID
	if err := a.st.UpsertMessage(store.Message{AccountID: acc, Folder: "INBOX", Role: store.RoleInbox, UID: 901, ThreadKey: "addr",
		FromName: "Lin", FromAddr: "lin@x.com", Subject: "收货地址", Snippet: "我是王小明",
		Body: "你好，我是王小明，请寄到北京市海淀区中关村大街1号。", Date: time.Now().Add(time.Hour),
		HTML: `<div><p>你好，我是<b>王小明</b>，请寄到北京市海淀区中关村大街1号。</p><a href="tel:13800138000" title="王小明">电话 13800138000</a></div>`}); err != nil {
		t.Fatal(err)
	}
	a.priv = privacy{View: true}
	a.reload()
	// without the model's files the patterns hide what they can, and the note says what is left
	a.open(0)
	if note, act := a.maskNote(); !strings.Contains(note, "还没有下载") || act != "下载" || !strings.Contains(a.thread[0].Body, "王小明") {
		t.Errorf("without the model's files: note %q, act %q, body %q", note, act, a.thread[0].Body)
	}

	linkModel(t, a.dir)
	a.reload()
	b := a.thread[len(a.thread)-1].Body
	if strings.Contains(b, "王小明") || strings.Contains(b, "海淀") || !strings.Contains(b, "‹PERSON_1›") {
		t.Errorf("model did not hide the message: %q", b)
	}
	h := a.thread[len(a.thread)-1].HTML // the page the web view loads
	for _, leak := range []string{"王小明", "海淀", "13800138000"} {
		if strings.Contains(h, leak) {
			t.Errorf("html keeps %q: %s", leak, h)
		}
	}
	if !strings.Contains(h, `<b><mark class="zm" data-k="PERSON" title="姓名">姓名 1</mark></b>`) || !strings.Contains(h, `href="tel:‹PHONE_1›"`) {
		t.Errorf("html: %s", h)
	}
	for _, m := range a.rows { // the list too
		if strings.Contains(m.Snippet, "王小明") {
			t.Errorf("snippet of the list not hidden: %q", m.Snippet)
		}
	}
	tt := ui.NewTester(a.view, 1360, 900)
	if err := tt.Click("收货地址"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(300e6)
	shot(t, tt, "13-hidden-model")
}
