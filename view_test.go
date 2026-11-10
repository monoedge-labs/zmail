package main

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

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
	a := newApp(t.TempDir(), st, svc, "zm_test", "127.0.0.1:0")
	t.Cleanup(func() { a.mcp.Stop() })
	return a
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

	if err := tt.Click("Agent 接入"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(600e6)
	if !tt.HasText("search_mail") || !tt.HasText("发送与删除不对外开放") {
		t.Fatalf("agent page not shown: %q", tt.Texts())
	}
	shot(t, tt, "4-agent")

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
	if err := tt.Click("Agent 接入"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(600e6)
	shot(t, tt, "7-dark-agent")
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
