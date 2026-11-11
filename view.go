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
	// What the last frame found in view is hidden first, and shows in this one.
	a.v = a.variant()
	a.unveil()
	// A message still to be hidden tells whether it shows a frame late (see
	// near): a frame that held one out of view is followed by another, a
	// moment later and one for all the frames until then, which asks for none.
	now := c.Now()
	settled := !a.settleAt.IsZero() && !now.Before(a.settleAt)
	if settled {
		a.settleAt = time.Time{}
	}
	a.late = false
	defer func() {
		if !a.late {
			return
		}
		if a.settleAt.IsZero() && !settled {
			a.settleAt = now.Add(settleDelay)
		}
		if !a.settleAt.IsZero() {
			c.After(a.settleAt.Sub(now))
		}
	}()

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
	a.linkSheet(c, p)
	a.feedbackSheet(c, p)
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
					a.navItem(c, p, "mail:junk", icoJunk, ui.Color{}, "垃圾邮件", 0, false)
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
					a.feedbackItem(c, p)
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
	case a.nav == "mail:junk":
		return "垃圾邮件"
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
		ui.Row(c).AlignItems(ui.Center).Gap(8).Padding(0, 8).DragWindow().Children(func() {
			ui.Text(c, a.title()).FontSize(20).FontWeight(600).TextColor(p.ink).Grow(1).SingleLine()
			if a.syncing {
				ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
					orbSpinner(c, p, syncOrb)
					muted(c, p, "同步中…")
				})
			} else {
				muted(c, p, fmt.Sprintf("%d 封未读", unread))
			}
			a.labelSwitch(c, p)
			a.shield(c, p, true)
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
				if a.byLabel() {
					for g, lg := range labelGroups(a.rows, a.labels) {
						name, dot := lg.Label, p.blue
						if name == "" {
							name, dot = "未标记", p.soft.Alpha(.7)
						}
						a.group(c, p, name, dot, len(lg.Rows), func() {
							a.rowGroup = g + 1
							defer func() { a.rowGroup = 0 }()
							for _, i := range lg.Rows {
								a.personRow(c, p, i)
							}
						})
					}
					return
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

// byLabel is whether the list is grouped by label now. It is only once some
// mail has a label, so that a user of none sees the usual groups.
func (a *app) byLabel() bool { return a.prefs.GroupByLabel && a.anyLabel }

// rowKey identifies a row among the others: the mail's ID, and its group
// when the list is grouped by label, as one mail may stand in several.
func (a *app) rowKey(id int64) any {
	if a.rowGroup == 0 {
		return id
	}
	return [2]int64{id, int64(a.rowGroup)}
}

// labelSwitch is the button in the list's header that groups the list by
// label. It shows once some mail has one.
func (a *app) labelSwitch(c *ui.Context, p pal) {
	if !a.anyLabel && !a.prefs.GroupByLabel {
		return
	}
	on := a.byLabel()
	state, tip, ink := "按标签分组：已关闭", "按标签分组", p.muted
	if on {
		state, tip, ink = "按标签分组：已开启", "按类别分组", p.blue
	}
	b := ui.ButtonBase(c).Height(30).Padding(0, 7).Radius(15).Label(state).Tooltip(tip)
	b.Children(func() { ui.Icon(c, icoTags).Size(16, 16).TextColor(ink) })
	switch {
	case on:
		b.Background(p.blue.Alpha(.14))
	case b.Hovered():
		b.Background(p.hover)
	}
	if b.Clicked() {
		a.prefs.GroupByLabel = !a.prefs.GroupByLabel
		a.prefs.save(a.dir)
	}
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
		Label(who(m) + "：" + m.Subject).Key(a.rowKey(m.ID))
	if a.veiled(m.ID, false) {
		a.near(c, b, m.ID, false, rowMargin)
	}
	b.Children(func() {
		avatar(c, p, who(m), 36, sel)
		ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
			ui.Row(c).AlignItems(ui.Baseline).Gap(8).Children(func() {
				ui.Text(c, who(m)).SingleLine().Grow(1).FontWeight(weight(!m.Seen, 600, 500)).TextColor(p.ink)
				muted(c, p, when(m.Date)).FontSize(12)
			})
			if a.veiled(m.ID, false) {
				live := a.asked[veilKey{m.ID, false, a.v}]
				bone(c, 14, 62, live)
				bone(c, 13, 88, live)
				return
			}
			maskedText(c, p, m.Subject, 0, weight(!m.Seen, 500, 400), p.ink).SingleLine()
			maskedText(c, p, m.Snippet, 13, 400, p.muted).SingleLine()
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

// A row is hidden once it is within rowMargin of showing, so that it is by
// the time it is scrolled to; a mail of the thread within mailMargin. A mail
// still to be hidden holds mailHold of height, so that a long thread does not
// bring all of its mails into view at once.
const (
	rowMargin  = 400
	mailMargin = 300
	mailHold   = 360
)

// settleDelay is how long after a frame the one that finds what it showed
// comes: short enough to pass unseen, long enough that moving the pointer
// does not draw every frame twice.
const settleDelay = 50 * time.Millisecond

// A mail hidden sooner than beamDelay shows neither the beam nor the wave:
// most are, and light that comes and goes within a moment is noise. The beam
// then comes up over beamFade.
const (
	beamDelay = 150 * time.Millisecond
	beamFade  = 250 * time.Millisecond
)

// bone holds the place of a line of text still to be hidden, of that size: a
// bar across share percent of the row, in a row as tall as the line, so that
// nothing moves when the text comes. It pulses while live, as the model
// reads the text, and is still otherwise: a row out of view waits unseen.
func bone(c *ui.Context, size, share float32, live bool) ui.Element {
	anim := display.SkeletonStatic
	if live {
		anim = display.SkeletonFade
	}
	row := ui.Row(c).FillWidth().AlignItems(ui.Center)
	row.Children(func() {
		ui.Text(c, " ").FontSize(size)
		ui.Box(c).WidthPercent(share).Children(func() {
			display.Skeleton(c, display.SkeletonProps{Height: size - 3, Animation: anim})
		})
	})
	return row
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
	if a.veiled(m.ID, false) {
		a.near(c, b, m.ID, false, rowMargin)
	}
	b.Children(func() {
		if a.veiled(m.ID, false) {
			bone(c, 14, 55, a.asked[veilKey{m.ID, false, a.v}]).Grow(1).MinWidth(0)
		} else {
			maskedText(c, p, m.Subject, 0, weight(!m.Seen, 500, 400), p.ink).SingleLine().Grow(1).MinWidth(0)
		}
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
		if a.priv.View {
			a.maskBanner(c, p)
		}
		if a.veiled(a.cur.ID, false) {
			bone(c, 24, 46, true)
		} else {
			maskedText(c, p, a.cur.Subject, 24, 600, p.ink).LineHeight(1.35)
		}
		// One beam at a time: on the first mail the model is reading.
		lit := int64(0)
		for _, m := range a.thread {
			if _, ok := a.pendAt[m.ID]; ok && a.veiled(m.ID, true) {
				lit = m.ID
				break
			}
		}
		// The scroll bar runs in the pane's margin, clear of the mails.
		ui.Scroll(c).Grow(1).Margin(0, -scrollGutter, 0, 0).Children(func() {
			ui.Column(c).Padding(0, scrollGutter, 0, 0).Children(func() {
				for i, m := range a.thread {
					if i > 0 { // a line between mails, plain enough to find the next one
						ui.Box(c).Height(1).Background(p.rule).Margin(28, 0)
					}
					a.message(c, p, m, m.ID == lit)
				}
			})
		})
	})
}

// shield is the switch of hiding private values in the mail shown: a pill
// that is blue and says 已打码 while it is on, and plain, saying 打码, when it
// is off, so that its state is seen at a glance.
func (a *app) shield(c *ui.Context, p pal, compact bool) {
	on := a.priv.View
	label, tip, ink, icon := "打码", "隐藏邮件里的邮箱、手机号、证件号、薪资、金额、密钥和验证码；本地模型 AstrLink Guard 还会隐藏姓名和地址", p.muted, icoShieldOff
	state := "打码：已关闭"
	if on {
		label, tip, ink, icon = "已打码", "点击显示原文", p.blue, icoShield
		state = "打码：已开启"
	}
	b := ui.ButtonBase(c).Height(30).Padding(0, 10).Gap(6).Radius(15).Label(state).Tooltip(tip)
	if compact {
		b.Padding(0, 7)
	}
	b.Children(func() {
		ui.Icon(c, icon).Size(16, 16).TextColor(ink)
		if !compact {
			ui.Text(c, label).FontSize(13).FontWeight(600).TextColor(ink)
		}
	})
	switch {
	case on:
		b.Background(p.blue.Alpha(.14))
	case b.Hovered():
		b.Background(p.hover)
	}
	if b.Clicked() {
		next := a.priv
		next.View = !next.View
		a.setPrivacy(next)
	}
}

// maskBanner tells, over the mail, that its values are hidden and how many,
// what falls short of that while the model is not there, and shows the values
// again on a click.
func (a *app) maskBanner(c *ui.Context, p pal) {
	text := fmt.Sprintf("已隐藏 %d 项敏感信息", a.hidden)
	switch {
	case a.hiding:
		text = "正在识别敏感信息…"
	case a.rest > 0:
		text += fmt.Sprintf("，其余 %d 封滚动到时识别", a.rest)
	case a.hidden == 0:
		text = "已打码，这封邮件里没有发现敏感信息"
	}
	if a.useModel() && !a.hiding {
		text += " · 姓名和地址由本地模型 AstrLink Guard 识别"
	}
	note, act := a.maskNote()
	tint, ink := p.blue.Alpha(.1), p.blue
	if note != "" {
		text, tint, ink = text+" · "+note, p.orange.Alpha(.12), p.orange
	}
	button := func(label string, strong bool) bool {
		b := ui.ButtonBase(c).Height(26).Padding(0, 10).Radius(13).Label(label)
		b.Children(func() { ui.Text(c, label).FontSize(13).FontWeight(600).TextColor(ink) })
		if b.Hovered() {
			b.Background(ink.Alpha(.14))
		}
		return b.Clicked()
	}
	ui.Row(c).AlignItems(ui.Center).Gap(8).Padding(8, 8, 8, 14).Radius(12).Background(tint).Children(func() {
		ui.Icon(c, icoShield).Size(16, 16).TextColor(ink)
		ui.Text(c, text).FontSize(13).TextColor(ink).Grow(1)
		if act != "" && button(act, false) {
			next := a.priv
			switch act {
			case "开启":
				next.ModelOff = false
			default: // 下载, 重试
				a.dl.err = ""
				a.vm.reset()
				a.downloadModel()
			}
			a.setPrivacy(next)
			a.reload()
		}
		if button("显示原文", true) {
			next := a.priv
			next.View = false
			a.setPrivacy(next)
		}
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
		if a.cur.Role == store.RoleJunk {
			jp := big
			jp.Title = "不是垃圾邮件，移回收件箱"
			if action.ActionIcon(c, icoInbox, jp).Clicked() {
				a.moveThread("INBOX")
			}
		}
		a.shield(c, p, false)
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

// message shows a mail of the thread. One still to be hidden shows as a card
// of lines to come; lit, it is the one the beam goes around while the model
// reads it. One just hidden after such a wait is crossed by the wave.
func (a *app) message(c *ui.Context, p pal, m store.Message, lit bool) {
	col := ui.Column(c).Gap(20).Key(m.ID)
	veiled := a.veiled(m.ID, true)
	if veiled {
		a.near(c, col.MinHeight(mailHold), m.ID, true, mailMargin)
	}
	col.Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(12).Children(func() {
			avatar(c, p, who(m), 40, true)
			ui.Column(c).Children(func() {
				ui.Text(c, who(m)).FontWeight(600).TextColor(p.ink)
				muted(c, p, fmt.Sprintf("%s · %s · %s", m.FromAddr, a.acctByID(m.AccountID), whenLong(m.Date)))
			})
		})
		switch pg := a.pages[pageKey{m.ID, a.v}]; {
		case veiled:
			a.mailHolder(c, p, m.ID, lit)
		case pg != nil:
			wv := ui.WebView(c, pg.view).Height(float32(pg.h.Load())).Radius(8)
			// A page's pixels cannot be moved: the wave's light goes over it,
			// once the page is laid out.
			if at, ok := a.waving(c, m.ID, pg.laid.Load()); ok {
				wv.Children(func() {
					// The box around it cuts the light to the page's corners.
					ui.Box(c).Absolute().Fill().Radius(8).Clip().IgnoreInput(true).Children(func() {
						ui.Box(c).Fill().Draw(func(pt *ui.Painter, r ui.Rect) {
							// The wave crosses what shows of the page: across
							// all of a long one it would pass the window in a
							// frame.
							v := pt.Visible()
							top, bottom := max(r.Y, v.Y), min(r.Y+r.H, v.Y+v.H)
							if bottom > top {
								r.Y, r.H = top, bottom-top
							}
							waveOverlay(pt, r, pt.Now().Sub(at), ui.RGB(255, 255, 255))
						})
					})
				})
			}
		default:
			body := ui.Column(c).MaxWidth(680)
			if at, ok := a.waving(c, m.ID, true); ok {
				body.Effect(waveEffect(c, c.Now().Sub(at)))
			}
			body.Children(func() {
				if a.priv.View && m.HTML == "" {
					plainBody(c, p, m.Body)
				} else {
					markdown.Markdown(c, m.Body, markdown.MarkdownProps{FontSize: 15, LineHeight: 1.7, HeaderMultiple: 0.4, MarginMultiple: 1.2, LoadImage: loadImage, OnLink: a.openLink})
				}
			})
		}
		if m.HandledBy == store.ActorAgent && m.Seen {
			ui.Row(c).AlignItems(ui.Center).Gap(8).MaxWidth(640).Padding(16, 0, 0).BorderWidth(1, 0, 0, 0).BorderColor(p.hair).Children(func() {
				ui.Icon(c, icoEye).Size(16, 16).TextColor(p.muted)
				muted(c, p, "agent 已读取并标记了这封邮件")
			})
		}
	})
}

// mailHolder stands for the body of a mail still to be hidden: a card of the
// lines to come. While the model reads the mail the lines pulse, and once it
// has waited long enough to be seen (beamDelay) the beam goes around the
// card, when it is the lit one.
func (a *app) mailHolder(c *ui.Context, p pal, id int64, lit bool) {
	since, reading := a.pendAt[id]
	anim := display.SkeletonStatic
	if reading {
		anim = display.SkeletonFade
	}
	card := ui.Column(c).MaxWidth(680).Padding(22, 24).Radius(holderRadius).Background(p.card).Border(1, p.hair)
	if lit && reading {
		if wait := beamDelay - time.Since(since); wait > 0 {
			c.After(wait)
		} else {
			dark := p.dark
			card.DrawOver(func(pt *ui.Painter, r ui.Rect) {
				on := time.Since(since) - beamDelay
				beam(pt, r, holderRadius, on.Seconds(), min(float32(on)/float32(beamFade), 1), dark)
			})
		}
	}
	card.Children(func() {
		display.SkeletonText(c, display.SkeletonTextProps{Rows: 5, FontSize: 15, Animation: anim})
	})
}

const holderRadius = 14

// scrollGutter is the room the reader's scroll bar has beside the mails.
const scrollGutter = 16

// waving reports whether the wave is crossing the mail of id, and since when.
// The wave begins with the first frame that can draw it: ready says the mail
// is laid out. It keeps the frames coming while it runs.
func (a *app) waving(c *ui.Context, id int64, ready bool) (time.Time, bool) {
	at, ok := a.waveAt[id]
	if !ok || !ready {
		return at, false
	}
	if at.IsZero() {
		at = c.Now()
		a.waveAt[id] = at
	}
	if c.Now().Sub(at) >= waveDuration || c.Preferences().ReduceMotion {
		delete(a.waveAt, id)
		return at, false
	}
	c.After(time.Second / 60)
	return at, true
}

// feedbackItem opens the project's new-issue page in the browser, where the
// user edits the text and submits it. Nothing is sent from here.
func (a *app) feedbackItem(c *ui.Context, p pal) {
	b := ui.ButtonBase(c).FillWidth().MinHeight(44).Padding(0, 12).Gap(10).Radius(12).Justify(ui.Start).
		Label("反馈问题").Tooltip("写下问题或想法，可选 AI 润色，再到 GitHub 提交 issue；内容由你确认后才提交")
	b.Children(func() {
		ui.Icon(c, icoFeedback).Size(18, 18).TextColor(p.nav)
		ui.Text(c, "反馈问题").SingleLine().Grow(1).FontWeight(500).TextColor(p.nav)
	})
	if b.Hovered() {
		b.Background(p.hover)
	}
	if b.Clicked() {
		a.fb.open = true
	}
}
