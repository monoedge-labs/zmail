package main

import (
	"fmt"
	"time"

	"github.com/monoedge-labs/zmail/internal/mailbox"
	"github.com/monoedge-labs/zmail/internal/store"
)

// quiet is a server that accepts every change and has no mail of its own,
// for --demo.
type quiet struct{}

func (quiet) Folders() ([]mailbox.Folder, error) {
	return []mailbox.Folder{{Name: "INBOX", Role: store.RoleInbox}, {Name: "Archive", Role: store.RoleArchive}, {Name: "Sent", Role: store.RoleSent}}, nil
}
func (quiet) Fetch(string, uint32, int) ([]mailbox.Header, error) { return nil, nil }
func (quiet) Flags(string, int) (map[uint32]bool, error)          { return nil, nil }
func (quiet) SetSeen(string, []uint32, bool) error                { return nil }
func (quiet) Move(_ string, uids []uint32, _ string) (map[uint32]uint32, error) {
	m := map[uint32]uint32{}
	for _, u := range uids {
		m[u] = u + 1000
	}
	return m, nil
}
func (quiet) Close() error { return nil }

type memSecrets map[string]string

func (m memSecrets) Get(id string) (string, error) { return m[id], nil }
func (m memSecrets) Set(id, s string) error        { m[id] = s; return nil }
func (m memSecrets) Delete(id string) error        { delete(m, id); return nil }

// demoService is a service on sample mail.
func demoService(st *store.Store) *mailbox.Service {
	svc := mailbox.New(st, memSecrets{})
	svc.Dial = func(store.Account, string) mailbox.Remote { return quiet{} }

	accs := []store.Account{
		{ID: "google:me@gmail.com", Provider: "google", Email: "me@gmail.com", Host: "imap.gmail.com:993"},
		{ID: "aliyun:me@aliyun.com", Provider: "aliyun", Email: "me@aliyun.com", Host: "imap.aliyun.com:993"},
		{ID: "qq:12345678@qq.com", Provider: "qq", Email: "12345678@qq.com", Host: "imap.qq.com:993"},
	}
	for _, a := range accs {
		st.UpsertAccount(a)
		st.SetFolder(a.ID, "INBOX", store.RoleInbox)
		st.SetFolder(a.ID, "Archive", store.RoleArchive)
		st.SetFolder(a.ID, "Sent", store.RoleSent)
	}
	st.SetFolder(accs[0].ID, "项目", store.RoleOther)

	type seed struct {
		acc              int
		name, addr, subj string
		body, cat        string
		ago              time.Duration
		seen             bool
		by               string
	}
	h := time.Hour
	seeds := []seed{
		{0, "林晓", "lin@acme.com", "设计评审改到周四 15:00", "周四下午三点可以吗？会议室已订好，[议程在这里](https://example.com/agenda)。\n\n另外请把 **Q3 的指标表**带上。", store.CatPerson, 25 * time.Minute, false, ""},
		{0, "GitHub", "noreply@github.com", "[zmail] CI 通过：main", "All checks passed.", store.CatNotification, 2 * h, false, ""},
		{1, "Stripe", "receipts@stripe.com", "你的 11 月收据", "已收到付款 \\$20.00。", store.CatNotification, 5 * h, true, store.ActorAgent},
		{2, "王磊", "wanglei@qq.com", "周末爬山？", "周六早上八点，老地方集合。", store.CatPerson, 8 * h, false, ""},
		{0, "Go Weekly", "news@golangweekly.com", "Go Weekly #581", "本周：Go 1.27 的新特性。", store.CatNewsletter, 20 * h, true, store.ActorAgent},
		{1, "阿里云", "service@aliyun.com", "账单提醒", "本月消费 ¥128.40。", store.CatNotification, 26 * h, true, store.ActorAgent},
		{0, "Anna", "anna@design.io", "Re: 图标稿", "改好了，看看这版。", store.CatPerson, 30 * h, true, store.ActorMe},
		{2, "腾讯云", "noreply@tencent.com", "域名即将到期", "example.dev 将在 30 天后到期。", store.CatNotification, 50 * h, true, store.ActorAgent},
		{0, "Linear", "notifications@linear.app", "3 条新评论", "有人评论了 ZM-12。", store.CatNotification, 52 * h, true, store.ActorAgent},
		{1, "知乎周刊", "weekly@zhihu.com", "本周热门", "精选 10 条。", store.CatNewsletter, 75 * h, true, store.ActorMe},
		{0, "林晓", "lin@acme.com", "Q3 指标表", "见附件。", store.CatPerson, 98 * h, true, store.ActorMe},
		{2, "京东", "jd@jd.com", "订单已发货", "预计明日送达。", store.CatNotification, 120 * h, true, store.ActorAgent},
		{0, "GitHub", "noreply@github.com", "[zmail] 新 issue #4", "Gmail 的 OAuth 支持？", store.CatNotification, 150 * h, true, store.ActorAgent},
		{1, "Medium", "digest@medium.com", "为你推荐", "5 篇文章。", store.CatNewsletter, 170 * h, true, store.ActorAgent},
		{0, "Anna", "anna@design.io", "图标稿", "初稿在这里。", store.CatPerson, 200 * h, true, store.ActorMe},
	}
	now := time.Now()
	for i, s := range seeds {
		a := accs[s.acc]
		if err := st.UpsertMessage(store.Message{
			AccountID: a.ID, Folder: "INBOX", Role: store.RoleInbox, UID: uint32(i + 1),
			ThreadKey: mailbox.ThreadKey(s.subj), FromName: s.name, FromAddr: s.addr, Subject: s.subj,
			Snippet: mailbox.Snippet(mailbox.PlainText(s.body), 140), Body: s.body, Date: now.Add(-s.ago), Seen: false, Category: s.cat,
		}); err != nil {
			panic(err)
		}
	}
	for i, s := range seeds {
		if !s.seen {
			continue
		}
		ms, _ := st.Search(store.Query{AccountID: accs[s.acc].ID, Role: store.RoleInbox, Limit: 100})
		for _, m := range ms {
			if m.UID == uint32(i+1) {
				st.MarkSeen([]int64{m.ID}, true, s.by)
			}
		}
	}
	for i, o := range []store.Op{
		{Actor: store.ActorAgent, Client: "claude-code", Tool: "search_mail", Detail: "query=收据", OK: true},
		{Actor: store.ActorAgent, Client: "claude-code", Tool: "mark_read", Detail: "3 messages, seen=true", OK: true},
		{Actor: store.ActorAgent, Client: "claude-code", Tool: "archive_thread", Detail: "2 messages", OK: true},
		{Actor: store.ActorAgent, Client: "codex", Tool: "get_stats", Detail: fmt.Sprint(7, " days"), OK: true},
	} {
		_ = i
		st.LogOp(o)
	}
	return svc
}
