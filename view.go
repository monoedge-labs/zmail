package main

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/lobehub/lobe-mygo/pkg/action"
	"github.com/lobehub/lobe-mygo/pkg/display"
	"github.com/lobehub/lobe-mygo/pkg/field"
	"github.com/lobehub/lobe-mygo/pkg/markdown"
	"github.com/lobehub/lobe-mygo/pkg/overlay"
	"github.com/lobehub/lobe-mygo/pkg/theme"

	"github.com/monoedge-labs/zmail/internal/store"
)

// The window is the design's frosted sheet: the system's material blurs the
// desktop behind it (the design's colored ground stands for that desktop),
// with the sidebar flat on the sheet and the pages as panes on it. Controls
// come from Lobe MyGo, whose theme follows the system's light or dark
// appearance.

func (a *app) view(c *ui.Context) {
	theme.Apply(c)
	p := palFor(theme.TokensOf(c).Dark)

	// The window's own shortcuts; ⌘F is the search field's.
	c.OnWindowShortcut(ui.Cmd, ui.KeyN, func() { a.add.open = true })
	c.OnWindowShortcut(ui.Cmd, ui.KeyR, a.syncNow)
	c.OnWindowShortcut(ui.Cmd, ui.KeyE, a.archive)
	c.OnWindowShortcut(ui.Cmd, ui.KeyU, a.toggleRead)

	if c.Vibrancy() {
		c.Root().Background(ui.Transparent)
	} else {
		c.Root().Background(p.base)
	}
	ui.Row(c).Fill().AlignItems(ui.Stretch).Background(p.sheetTint).Children(func() {
		a.sidebar(c, p)
		a.router.View(c, func(r *ui.Route) {
			switch {
			case r.Match("/stats"):
				r.Title("统计")
				a.statsPage(c, p)
			default:
				r.Title("收件箱")
				a.mailPage(c, p)
			}
		})
	})
	a.addSheet(c, p)
}

// ---- names and dates

var providerName = map[string]string{"google": "Gmail", "aliyun": "阿里邮箱", "qq": "QQ 邮箱", "icloud": "iCloud"}

// acctLabel names an account by its service, or by its address when two
// accounts share one.
func (a *app) acctLabel(match func(store.Account) bool) string {
	for _, acc := range a.accounts {
		if !match(acc) {
			continue
		}
		n := 0
		for _, o := range a.accounts {
			if o.Provider == acc.Provider {
				n++
			}
		}
		if n > 1 {
			return acc.Email
		}
		return providerName[acc.Provider]
	}
	return ""
}

func (a *app) acctByID(id string) string {
	return a.acctLabel(func(x store.Account) bool { return x.ID == id })
}
func (a *app) acctByEmail(e string) string {
	if l := a.acctLabel(func(x store.Account) bool { return x.Email == e }); l != "" {
		return l
	}
	return e
}

func who(m store.Message) string {
	if m.FromName != "" {
		return m.FromName
	}
	return m.FromAddr
}

func sameDay(a, b time.Time) bool {
	return a.Year() == b.Year() && a.YearDay() == b.YearDay()
}

var weekday = []string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}

// when is the short time of a row: the hour today, then "昨天", the weekday
// within a week, the date after.
func when(d time.Time) string {
	now := time.Now()
	switch {
	case sameDay(d, now):
		return d.Format("15:04")
	case sameDay(d, now.AddDate(0, 0, -1)):
		return "昨天"
	case now.Sub(d) < 6*24*time.Hour:
		return weekday[d.Weekday()]
	case d.Year() == now.Year():
		return d.Format("1月2日")
	}
	return d.Format("2006/1/2")
}

func whenLong(d time.Time) string {
	now := time.Now()
	switch {
	case sameDay(d, now):
		return "今天 " + d.Format("15:04")
	case sameDay(d, now.AddDate(0, 0, -1)):
		return "昨天 " + d.Format("15:04")
	}
	return d.Format("1月2日 15:04")
}

// ---- sidebar

// The sidebar's background moves the window, as a title bar does; its top
// keeps clear of the traffic lights.
func (a *app) sidebar(c *ui.Context, p pal) {
	top := max(c.TitleBar().Height+4, 16)
	ui.Column(c).Width(216).Padding(top, 12, 16).Gap(18).DragWindow().Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(8).Padding(0, 4, 0, 12).Children(func() {
			ui.Image(c, logo).Size(28, 28).Label("Zmail")
			ui.Text(c, "Zmail").FontSize(18).FontWeight(600).TextColor(p.ink).Grow(1)
			add := ui.ButtonBase(c).Size(34, 34).Radius(12).Background(p.blue).
				Shadow(0, 5, 14, 0, p.blue.Alpha(.35)).Label("添加账户").Tooltip("添加账户  ⌘N")
			add.Children(func() { ui.Icon(c, icoPlus).Size(16, 16).TextColor(ui.RGB(255, 255, 255)) })
			if add.Clicked() {
				a.add.open = true
			}
		})

		ui.Scroll(c).Grow(1).Children(func() {
			ui.Column(c).Gap(18).Children(func() {
				ui.Column(c).Gap(2).Children(func() {
					a.navItem(c, p, "mail:inbox", icoInbox, ui.Color{}, "收件箱", a.total, true)
					a.navItem(c, p, "mail:sent", icoSent, ui.Color{}, "已发送", 0, false)
					a.navItem(c, p, "mail:archive", icoArchive, ui.Color{}, "归档", 0, false)
				})
				if len(a.folders) > 0 {
					ui.Column(c).Gap(2).Children(func() {
						heading(c, p, "文件夹")
						for _, f := range a.folders {
							a.navItem(c, p, "folder:"+f, icoFolder, ui.Color{}, f, 0, false)
						}
					})
				}
				ui.Column(c).Gap(2).Children(func() {
					ui.Row(c).AlignItems(ui.Center).Children(func() {
						heading(c, p, "账户").Grow(1)
						if action.ActionIcon(c, icoPlus, action.ActionIconProps{Title: "添加账户"}).Clicked() {
							a.add.open = true
						}
					})
					for _, acc := range a.accounts {
						b, ok := brands[acc.Provider]
						if !ok {
							b.icon = icoMail
						}
						a.navItem(c, p, "acct:"+acc.ID, b.icon, b.color, a.acctByID(acc.ID), a.unread[acc.ID], false)
					}
				})
				ui.Column(c).Gap(2).Padding(14, 0, 0).BorderWidth(1, 0, 0, 0).BorderColor(p.hair).Children(func() {
					a.navItem(c, p, "page:stats", icoChart, ui.Color{}, "统计", 0, false)
				})
			})
		})
	})
}

func initial(s string) string {
	for _, r := range s {
		return string(r)
	}
	return "?"
}

func muted(c *ui.Context, p pal, s string) ui.Element {
	return ui.Text(c, s).FontSize(13).TextColor(p.muted)
}

func heading(c *ui.Context, p pal, s string) ui.Element {
	return ui.Text(c, s).FontSize(12).FontWeight(600).TextColor(p.muted).LetterSpacing(0.5).Padding(0, 12, 4)
}

// navItem is a row of the sidebar; tint, when set, is its icon's own color,
// as a mail service's mark keeps.
func (a *app) navItem(c *ui.Context, p pal, id string, ic *ui.SVG, tint ui.Color, label string, count int, hot bool) {
	sel := a.nav == id
	b := ui.ButtonBase(c).FillWidth().MinHeight(44).Padding(0, 12).Gap(10).Radius(12).Justify(ui.Start).Label(label).Key(id)
	b.Children(func() {
		col := p.nav
		switch {
		case tint != ui.Color{}:
			col = tint
		case sel:
			col = p.blue
		}
		ui.Icon(c, ic).Size(18, 18).TextColor(col)
		w, col := 500, p.nav
		if sel {
			w, col = 600, p.ink
		}
		ui.Text(c, label).SingleLine().Grow(1).FontWeight(w).TextColor(col)
		if count > 0 {
			t := ui.Text(c, fmt.Sprint(count)).FontSize(12).TextColor(p.soft)
			if hot {
				t.TextColor(p.blue).FontWeight(600)
			}
		}
	})
	switch {
	case sel:
		b.Background(p.blue.Alpha(.16))
	case b.Hovered():
		b.Background(p.hover)
	}
	if b.Clicked() {
		a.navigate(id)
	}
}

// ---- mail

func (a *app) mailPage(c *ui.Context, p pal) {
	ui.Row(c).Grow(1).Gap(8).Padding(8).AlignItems(ui.Stretch).Children(func() {
		a.listPane(c, p)
		a.readerPane(c, p)
	})
}

func (a *app) title() string {
	switch {
	case a.nav == "mail:sent":
		return "已发送"
	case a.nav == "mail:archive":
		return "归档"
	case strings.HasPrefix(a.nav, "folder:"):
		return strings.TrimPrefix(a.nav, "folder:")
	case strings.HasPrefix(a.nav, "acct:"):
		return a.acctByID(strings.TrimPrefix(a.nav, "acct:"))
	}
	return "收件箱"
}

// groups splits the rows by category, and counts the unread ones.
func (a *app) groups() (people, notes, news []int, unread int) {
	for i, m := range a.rows {
		switch m.Category {
		case store.CatNotification:
			notes = append(notes, i)
		case store.CatNewsletter:
			news = append(news, i)
		default:
			people = append(people, i)
		}
		if !m.Seen {
			unread++
		}
	}
	return
}

func (a *app) listPane(c *ui.Context, p pal) {
	_, _, _, unread := a.groups()
	ui.Column(c).Basis(340).Grow(1).MinWidth(0).Gap(16).Padding(16, 12).Radius(16).
		Background(p.listBg).Border(1, p.listEdge).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Padding(0, 8).DragWindow().Children(func() {
			ui.Text(c, a.title()).FontSize(20).FontWeight(600).TextColor(p.ink).Grow(1).SingleLine()
			status := fmt.Sprintf("%d 封未读", unread)
			if a.syncing {
				status = "同步中…"
			}
			muted(c, p, status)
		})
		before := a.query
		field.SearchBar(c, &a.query, field.SearchBarProps{Placeholder: "搜索邮件", ShortKey: "mod+f", Size: theme.Large, Variant: field.InputFilled})
		if a.query != before {
			a.reload()
		}
		if a.notice != "" {
			ui.Text(c, a.notice).FontSize(12).TextColor(p.danger)
		}

		// The search above may have reloaded the rows: group them after it.
		people, notes, news, _ := a.groups()
		ui.Scroll(c).Grow(1).Children(func() {
			ui.Column(c).Gap(16).Children(func() {
				if len(a.rows) == 0 {
					text := "没有邮件"
					if len(a.accounts) == 0 {
						text = "还没有账户，点左上角的 + 添加"
					}
					display.Empty(c, display.EmptyProps{Icon: icoInbox, Title: text})
				}
				if len(people) > 0 {
					a.group(c, p, "人", p.blue, len(people), func() {
						for _, i := range people {
							a.personRow(c, p, i)
						}
					})
				}
				if len(notes) > 0 {
					a.group(c, p, "通知", p.muted, len(notes), func() {
						for _, i := range notes {
							a.compactRow(c, p, i)
						}
					})
				}
				if len(news) > 0 {
					a.group(c, p, "订阅", p.soft.Alpha(.7), len(news), func() { a.newsRows(c, p, news) })
				}
			})
		})
	})
}

func (a *app) group(c *ui.Context, p pal, name string, dot ui.Color, n int, rows func()) {
	ui.Column(c).Gap(4).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(8).Padding(4, 8).Children(func() {
			ui.Box(c).Size(8, 8).Radius(4).Background(dot)
			ui.Text(c, name).FontSize(12).FontWeight(600).TextColor(p.soft).LetterSpacing(0.5)
			muted(c, p, fmt.Sprint(n)).FontSize(12)
		})
		rows()
	})
}

func (a *app) personRow(c *ui.Context, p pal, i int) {
	m := a.rows[i]
	sel := a.hasCur && a.cur.ID == m.ID
	b := ui.ButtonBase(c).FillWidth().Gap(12).Padding(12).Radius(14).Justify(ui.Start).AlignItems(ui.Start).
		Label(who(m) + "：" + m.Subject).Key(m.ID)
	b.Children(func() {
		avatar(c, p, who(m), 36, sel)
		ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
			ui.Row(c).AlignItems(ui.Baseline).Gap(8).Children(func() {
				ui.Text(c, who(m)).SingleLine().Grow(1).FontWeight(weight(!m.Seen, 600, 500)).TextColor(p.ink)
				muted(c, p, when(m.Date)).FontSize(12)
			})
			ui.Text(c, m.Subject).SingleLine().FontWeight(weight(!m.Seen, 500, 400)).TextColor(p.ink)
			muted(c, p, m.Snippet).SingleLine()
		})
	})
	switch {
	case sel:
		b.Background(p.selCard).Shadow(0, 6, 18, 0, p.shadow)
	case b.Hovered():
		b.Background(p.hover)
	}
	if b.Clicked() {
		a.open(i)
	}
}

func weight(strong bool, hi, lo int) int {
	if strong {
		return hi
	}
	return lo
}

func (a *app) compactRow(c *ui.Context, p pal, i int) {
	m := a.rows[i]
	sel := a.hasCur && a.cur.ID == m.ID
	b := ui.ButtonBase(c).FillWidth().MinHeight(44).Gap(12).Padding(0, 12).Radius(12).Justify(ui.Start).Label(m.Subject).Key(m.ID)
	b.Children(func() {
		ui.Text(c, m.Subject).SingleLine().Grow(1).MinWidth(0).FontWeight(weight(!m.Seen, 500, 400)).TextColor(p.ink)
		muted(c, p, when(m.Date)).FontSize(12)
	})
	switch {
	case sel:
		b.Background(p.selCard).Shadow(0, 6, 18, 0, p.shadow)
	case b.Hovered():
		b.Background(p.hover)
	}
	if b.Clicked() {
		a.open(i)
	}
}

// newsRows folds the newsletters into one row, which opens to list them.
func (a *app) newsRows(c *ui.Context, p pal, idx []int) {
	unread := 0
	for _, i := range idx {
		if !a.rows[i].Seen {
			unread++
		}
	}
	sub := "已全部标记为已读"
	if unread > 0 {
		sub = fmt.Sprintf("%d 封未读", unread)
	}
	b := ui.ButtonBase(c).FillWidth().Column().AlignItems(ui.Start).Gap(2).Padding(12).Radius(12).Label(fmt.Sprintf("%d 封订阅邮件", len(idx)))
	b.Children(func() {
		ui.Text(c, fmt.Sprintf("%d 封订阅邮件", len(idx))).TextColor(p.ink)
		muted(c, p, sub)
	})
	if b.Hovered() {
		b.Background(p.hover)
	}
	if b.Clicked() {
		a.newsOpen = !a.newsOpen
	}
	if a.newsOpen {
		for _, i := range idx {
			a.compactRow(c, p, i)
		}
	}
}

func (a *app) readerPane(c *ui.Context, p pal) {
	pane := ui.Column(c).Basis(480).Grow(3).MinWidth(0).Gap(20).Padding(12, 40, 40).Radius(16).
		Background(p.readerBg).Border(1, p.readerEdge).Shadow(0, 8, 28, 0, p.shadowSoft)
	pane.Children(func() {
		if !a.hasCur {
			ui.Column(c).Grow(1).Center().DragWindow().Children(func() {
				display.Empty(c, display.EmptyProps{Icon: icoMail, Title: "选择一封邮件"})
			})
			return
		}
		a.toolbar(c, p)
		ui.Text(c, a.cur.Subject).FontSize(24).FontWeight(600).LineHeight(1.35).TextColor(p.ink)
		ui.Scroll(c).Grow(1).Children(func() {
			ui.Column(c).Gap(24).Children(func() {
				for _, m := range a.thread {
					a.message(c, p, m)
				}
			})
		})
	})
}

func (a *app) toolbar(c *ui.Context, p pal) {
	big := action.ActionIconProps{Size: theme.Large}
	ui.Row(c).Gap(4).Margin(0, -12).DragWindow().Children(func() {
		props := big
		props.Title = "归档（⌘E）"
		if action.ActionIcon(c, icoArchive, props).Clicked() {
			a.archive()
		}
		var items []overlay.MenuItem
		for _, f := range a.folders {
			items = append(items, overlay.MenuItem{Key: f, Label: f, Icon: icoFolder})
		}
		if len(items) > 0 {
			mp := big
			mp.Title = "移动到文件夹"
			if key := overlay.DropdownMenu(c, &a.moveOpen, overlay.DropdownMenuProps{Items: items}, func(t ui.Element) {
				action.StyleActionIcon(c, t, icoMove, mp)
			}); key != "" {
				a.moveThread(key)
			}
		}
		rp, ic := big, icoMailOpen
		rp.Title = "标为未读（⌘U）"
		if !a.cur.Seen {
			rp.Title, ic = "标为已读（⌘U）", icoEye
		}
		if action.ActionIcon(c, ic, rp).Clicked() {
			a.toggleRead()
		}
	})
}

var imageClient = &http.Client{Timeout: 20 * time.Second}

// loadImage fetches a picture of a message from the web, as a mail app
// does: its sender can tell when it is opened.
// ponytail: no cache across launches; add one when reopening mail is slow.
func loadImage(url string) (ui.ImageSource, error) {
	res, err := imageClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", url, res.Status)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, 10<<20))
	if err != nil {
		return nil, err
	}
	return ui.DecodeImage(b)
}

func (a *app) message(c *ui.Context, p pal, m store.Message) {
	ui.Column(c).Gap(20).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(12).Children(func() {
			avatar(c, p, who(m), 40, true)
			ui.Column(c).Children(func() {
				ui.Text(c, who(m)).FontWeight(600).TextColor(p.ink)
				muted(c, p, fmt.Sprintf("%s · %s · %s", m.FromAddr, a.acctByID(m.AccountID), whenLong(m.Date)))
			})
		})
		ui.Column(c).MaxWidth(680).Children(func() {
			markdown.Markdown(c, m.Body, markdown.MarkdownProps{FontSize: 15, LineHeight: 1.7, HeaderMultiple: 0.4, MarginMultiple: 1.2, LoadImage: loadImage})
		})
		if m.HandledBy == store.ActorAgent && m.Seen {
			ui.Row(c).AlignItems(ui.Center).Gap(8).MaxWidth(640).Padding(16, 0, 0).BorderWidth(1, 0, 0, 0).BorderColor(p.hair).Children(func() {
				ui.Icon(c, icoEye).Size(16, 16).TextColor(p.muted)
				muted(c, p, "agent 已读取并标记了这封邮件")
			})
		}
	})
}
