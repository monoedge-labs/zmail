package main

// seedDemo writes the same sample mail the desktop's -demo shows, so a
// first launch has something in it. Deleted on the first real account:
// the demo account's ID is real-looking but nothing syncs it.
//
// The seeds mirror ../demo.go's; keep the two in step.

import (
	"time"

	"github.com/monoedge-labs/zmail/internal/mailbox"
	"github.com/monoedge-labs/zmail/internal/store"
)

// quiet is a remote that accepts every change and has no mail of its
// own, for the demo account. (The same shape as ../demo.go's.)
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

func seedDemo(st *store.Store) {
	accs := []store.Account{
		{ID: "demo:zephyr@me.com", Provider: "demo", Email: "zephyr@me.com"},
	}
	for _, a := range accs {
		st.UpsertAccount(a)
		st.SetFolder(a.ID, "INBOX", store.RoleInbox)
		st.SetFolder(a.ID, "Archive", store.RoleArchive)
		st.SetFolder(a.ID, "Sent", store.RoleSent)
	}

	type seed struct {
		name, addr, subj string
		body, cat        string
		ago              time.Duration
		seen             bool
	}
	h := time.Hour
	seeds := []seed{
		{"林晓", "linxiao@aliyun.com", "设计评审改到周四 15:00", "周四下午三点可以吗？会议室已订好，[议程在这里](https://example.com/agenda)。\n\n另外请把 **Q3 的指标表**带上。", store.CatPerson, 25 * time.Minute, false},
		{"GitHub", "noreply@github.com", "[zmail] CI 通过：main", "All checks passed.", store.CatNotification, 2 * h, false},
		{"Google", "no-reply@accounts.google.com", "Google 验证码：482913", "你的 Google 验证码是 482913，10 分钟内有效。如果不是你本人操作，请忽略这封邮件。", store.CatNotification, 3 * h, false},
		{"Stripe", "receipts@stripe.com", "你的 11 月收据", "已收到付款 $20.00。", store.CatNotification, 5 * h, true},
		{"王芳", "wangfang@qq.com", "周末爬山？", "周六早上八点，老地方集合。", store.CatPerson, 8 * h, false},
		{"Go Weekly", "newsletter@golangweekly.com", "Go Weekly #581", "本周：Go 1.27 的新特性。", store.CatNewsletter, 20 * h, true},
		{"阿里云", "billing@notice.aliyun.com", "账单提醒", "本月消费 ¥128.40。", store.CatNotification, 26 * h, true},
		{"Anna", "anna@example.com", "Re: 图标稿", "改好了，看看这版。", store.CatPerson, 30 * h, true},
		{"Notion", "notify@mail.notion.so", "Your login code: 736128", "Use this one-time code to sign in. It expires in 10 minutes.", store.CatNotification, 28 * h, true},
		{"腾讯云", "noreply@tencent.com", "域名即将到期", "example.dev 将在 30 天后到期。", store.CatNotification, 50 * h, true},
		{"Linear", "notifications@linear.app", "3 条新评论", "有人评论了 ZM-12。", store.CatNotification, 52 * h, true},
		{"知乎周刊", "weekly@zhihu.com", "本周热门", "精选 10 条。", store.CatNewsletter, 75 * h, true},
		{"林晓", "linxiao@aliyun.com", "Q3 指标表", "见附件。", store.CatPerson, 98 * h, true},
		{"京东", "order@jd.com", "订单已发货", "预计明日送达。", store.CatNotification, 120 * h, true},
		{"GitHub", "noreply@github.com", "[zmail] 新 issue #4", "Gmail 的 OAuth 支持？", store.CatNotification, 150 * h, true},
		{"Medium", "noreply@medium.com", "为你推荐", "5 篇文章。", store.CatNewsletter, 170 * h, true},
		{"Anna", "anna@example.com", "图标稿", "初稿在这里。", store.CatPerson, 200 * h, true},
	}
	now := time.Now()
	for i, s := range seeds {
		a := accs[0]
		if err := st.UpsertMessage(store.Message{
			AccountID: a.ID, Folder: "INBOX", Role: store.RoleInbox, UID: uint32(i + 1),
			ThreadKey: mailbox.ThreadKey(s.subj), FromName: s.name, FromAddr: s.addr, Subject: s.subj,
			Snippet: mailbox.Snippet(mailbox.PlainText(s.body), 140), Body: s.body, Date: now.Add(-s.ago), Seen: s.seen, Category: s.cat,
		}); err != nil {
			panic(err)
		}
	}
}
