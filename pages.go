package main

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/lobehub/lobe-mygo/pkg/action"
	"github.com/lobehub/lobe-mygo/pkg/choice"
	"github.com/lobehub/lobe-mygo/pkg/feedback"
	"github.com/lobehub/lobe-mygo/pkg/field"
	"github.com/lobehub/lobe-mygo/pkg/form"
	"github.com/lobehub/lobe-mygo/pkg/theme"

	"github.com/monoedge-labs/zmail/internal/guard"
	"github.com/monoedge-labs/zmail/internal/mailbox"
)

// page is the frame of the statistics page: a title with what
// goes beside it, and the cards under it.
func page(c *ui.Context, p pal, title, sub string, side func(), body func()) {
	ui.Column(c).Grow(1).MinWidth(0).Padding(28, 32, 40).Gap(20).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(16).DragWindow().Children(func() {
			ui.Column(c).Grow(1).Gap(4).Children(func() {
				ui.Text(c, title).FontSize(24).FontWeight(600).TextColor(p.ink)
				ui.Text(c, sub).TextColor(p.soft)
			})
			if side != nil {
				side()
			}
		})
		ui.Scroll(c).Grow(1).Children(func() { ui.Column(c).Gap(16).Children(body) })
	})
}

// columns lays out cards side by side, sharing the width.
func columns(c *ui.Context, gap float32, cols ...func()) {
	ui.Row(c).Gap(gap).AlignItems(ui.Start).Children(func() {
		for _, col := range cols {
			ui.Column(c).Basis(280).Grow(1).MinWidth(0).Gap(gap).Children(col)
		}
	})
}

// ---- statistics

var toolLabel = map[string]string{
	"archive_thread": "归档", "move_to_folder": "移到文件夹", "mark_read": "标记已读", "label_mail": "打标签", "list_labels": "列出标签",
	"search_mail": "搜索", "read_thread": "读取会话", "list_folders": "列出文件夹", "get_stats": "查看统计",
}

func (a *app) statsPage(c *ui.Context, p pal) {
	s := a.stats
	days := rangeDays[a.range_]
	from := time.Now().AddDate(0, 0, -(days - 1))
	sub := fmt.Sprintf("%s – %s", from.Format("1 月 2 日"), time.Now().Format("1 月 2 日"))

	side := func() {
		before := a.range_
		action.Segmented(c, &a.range_, []string{"7 天", "30 天", "90 天"}, action.SegmentedProps{Size: theme.Large})
		if a.range_ != before {
			a.reload()
		}
	}
	page(c, p, "统计", sub, side, func() {
		handled := s.AgentHandled + s.MeHandled
		share := "—"
		if handled > 0 {
			share = fmt.Sprintf("%d%%", s.AgentHandled*100/handled)
		}
		ui.Row(c).Gap(16).Children(func() {
			for _, n := range [][2]string{{"收到", fmt.Sprint(s.Received)}, {"发出", fmt.Sprint(s.Sent)}, {"未读", fmt.Sprint(s.Unread)}, {"Agent 处理", share}} {
				panel(c, p, p.card).Grow(1).Basis(0).Gap(4).Padding(16, 20).Children(func() {
					ui.Text(c, n[0]).TextColor(p.soft)
					ui.Text(c, n[1]).FontSize(32).FontWeight(600).TextColor(p.ink)
				})
			}
		})
		columns(c, 16, func() {
			panel(c, p, p.cardHi).Gap(16).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(16).Children(func() {
					ui.Text(c, "每日处理的邮件").FontSize(16).FontWeight(600).TextColor(p.ink).Grow(1)
					legend(c, p, p.orange, fmt.Sprintf("Agent 处理 %d", s.AgentHandled))
					legend(c, p, p.blue, fmt.Sprintf("我处理 %d", s.MeHandled))
				})
				a.dayBars(c, p)
			})
			panel(c, p, p.card).Children(func() {
				ui.Text(c, "按账户").FontSize(16).FontWeight(600).TextColor(p.ink)
				var rows []barRow
				for email, n := range s.ByAccount {
					rows = append(rows, barRow{a.acctByEmail(email), n})
				}
				bars(c, p, rows)
			})
		}, func() {
			panel(c, p, p.card).Children(func() {
				ui.Text(c, "来信最多").FontSize(16).FontWeight(600).TextColor(p.ink)
				var rows []barRow
				for _, x := range s.TopSenders {
					n := x.Name
					if n == "" {
						n = x.Addr
					}
					rows = append(rows, barRow{n, x.Count})
				}
				bars(c, p, rows)
			})
			panel(c, p, p.card).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(12).Children(func() {
					a.faceView(c)
					ui.Text(c, "Agent 的操作").FontSize(16).FontWeight(600).TextColor(p.ink)
				})
				var ops []barRow
				for tool, n := range s.AgentOps {
					if toolLabel[tool] != "" && n > 0 {
						ops = append(ops, barRow{toolLabel[tool], n})
					}
				}
				sort.Slice(ops, func(i, j int) bool { return ops[i].n > ops[j].n })
				if len(ops) == 0 {
					muted(c, p, "这段时间没有操作")
				}
				for i, o := range ops {
					ui.Row(c).AlignItems(ui.Center).Padding(weightPad(i), 0, 0).Children(func() {
						ui.Text(c, o.label).TextColor(p.ink).Grow(1)
						ui.Text(c, fmt.Sprint(o.n)).FontSize(18).FontWeight(600).TextColor(p.ink)
					})
				}
				next := a.priv
				ui.Row(c).AlignItems(ui.Center).Gap(12).Padding(14, 0, 0).BorderWidth(1, 0, 0, 0).BorderColor(p.hair).Children(func() {
					ui.Text(c, "对 agent 隐藏敏感信息").FontSize(13).TextColor(p.soft).Grow(1).
						Tooltip("agent 读到的主题、摘要和正文里，邮箱、手机号、证件号、银行卡、密钥、验证码、密码、薪资和金额换成 ‹EMAIL_1› 这样的占位符；打开本地模型后还有姓名和地址")
					choice.Switch(c, &next.Agent, choice.SwitchProps{Size: theme.Small})
				})
				ui.Row(c).AlignItems(ui.Center).Gap(12).Children(func() {
					ui.Text(c, "用本地模型 AstrLink Guard 识别姓名和地址").FontSize(13).TextColor(p.soft).Grow(1).
						Tooltip("AstrLink Guard（QuantumNous 出品，Apache-2.0）是一个识别姓名、地址等个人信息的小模型，经 magpie 的纯 Go 读取器在本机运行，邮件内容不会离开电脑。对 agent 和窗口里的邮件都生效")
					on := next.model()
					choice.Switch(c, &on, choice.SwitchProps{Size: theme.Small})
					next.ModelOff = !on
				})
				a.modelStatus(c, p)
				ui.Row(c).AlignItems(ui.Center).Gap(12).Children(func() {
					ui.Text(c, "点击邮件里的链接前先确认").FontSize(13).TextColor(p.soft).Grow(1).
						Tooltip("邮件默认只读。链接可能是诱骗点击（钓鱼页、伪造登录），打开前会先显示完整网址并请你确认；关闭后点击链接直接在浏览器打开")
					guard := !next.NoLinkGuard
					choice.Switch(c, &guard, choice.SwitchProps{Size: theme.Small})
					next.NoLinkGuard = !guard
				})
				a.setPrivacy(next)
			})
		})
	})
}

func weightPad(i int) float32 {
	if i == 0 {
		return 0
	}
	return 12
}

func legend(c *ui.Context, p pal, col ui.Color, label string) {
	ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
		ui.Box(c).Size(10, 10).Radius(3).Background(col)
		ui.Text(c, label).FontSize(13).TextColor(p.soft)
	})
}

// dayBars stacks each day's handled mail: agents below, me above.
func (a *app) dayBars(c *ui.Context, p pal) {
	days := a.stats.Days
	peak := 1
	for _, d := range days {
		peak = max(peak, d.Agent+d.Me)
	}
	top := (peak + 9) / 10 * 10
	const h = 180
	scale := func(n int) float32 { return float32(n) * h / float32(top) }

	ui.Row(c).Gap(12).Children(func() {
		ui.Column(c).Height(h).Justify(ui.SpaceBetween).AlignItems(ui.End).Children(func() {
			for _, v := range []int{top, top / 2, 0} {
				ui.Text(c, fmt.Sprint(v)).FontSize(12).TextColor(p.muted)
			}
		})
		ui.Column(c).Grow(1).MinWidth(0).Gap(8).Children(func() {
			ui.Box(c).Height(h).Children(func() {
				for i := 0; i < 3; i++ {
					ui.Box(c).Absolute().Left(0).Right(0).Top(float32(i) * (h - 1) / 2).Height(1).Background(p.hair)
				}
				ui.Row(c).Fill().AlignItems(ui.End).Justify(ui.SpaceAround).Children(func() {
					for _, d := range days {
						ui.Column(c).Width(barWidth(len(days))).Justify(ui.End).Gap(2).
							Tooltip(fmt.Sprintf("%s · Agent %d · 我 %d", d.Day, d.Agent, d.Me)).Children(func() {
							ui.Box(c).Height(scale(d.Me)).Background(p.blue).Radius(4, 4, 0, 0)
							ui.Box(c).Height(scale(d.Agent)).Background(p.orange)
						})
					}
				})
			})
			ui.Row(c).Justify(ui.SpaceAround).Children(func() {
				for i, d := range days {
					label := ""
					if t, err := time.ParseInLocation("2006-01-02", d.Day, time.Local); err == nil {
						switch {
						case len(days) <= 7:
							label = weekday[t.Weekday()]
						case i%max(1, len(days)/6) == 0:
							label = t.Format("1/2")
						}
					}
					ui.Text(c, label).FontSize(12).TextColor(p.muted).Width(barWidth(len(days))).TextAlign(ui.Center).NoWrap()
				}
			})
		})
	})
}

func barWidth(n int) float32 {
	if n <= 7 {
		return 28
	}
	return max(3, 560/float32(n)-2)
}

type barRow struct {
	label string
	n     int
}

// bars draws one labelled horizontal bar per row, the largest full width.
func bars(c *ui.Context, p pal, rows []barRow) {
	sort.Slice(rows, func(i, j int) bool { return rows[i].n > rows[j].n })
	if len(rows) == 0 {
		muted(c, p, "这段时间没有数据")
		return
	}
	peak := max(1, rows[0].n)
	ui.Column(c).Gap(12).Children(func() {
		for _, r := range rows {
			ui.Row(c).AlignItems(ui.Center).Gap(12).Children(func() {
				ui.Text(c, r.label).SingleLine().Width(72).TextColor(p.ink)
				ui.Box(c).Grow(1).Height(8).Children(func() {
					ui.Box(c).WidthPercent(float32(r.n)*100/float32(peak)).Height(8).Radius(0, 4, 4, 0).Background(p.blue)
				})
				ui.Text(c, fmt.Sprint(r.n)).Width(32).TextColor(p.soft).TextAlign(ui.End)
			})
		}
	})
}

// ---- add an account

type service struct{ id, tile, name, hint string }

var services = []service{
	{"google", "G", "Google", "Gmail、Workspace · 应用专用密码"},
	{"aliyun", "阿", "阿里邮箱", "企业版用客户端专用密码，个人版用登录密码"},
	{"qq", "Q", "QQ 邮箱", "QQ、Foxmail · 授权码"},
	{"icloud", "i", "iCloud 邮箱", "@icloud.com、@me.com · App 专用密码"},
}

func (a *app) addSheet(c *ui.Context, p pal) {
	s := &a.add
	if !s.open {
		s.step = 0
		return
	}
	// A sheet over the window, on an opaque ground: what is behind a dialog
	// must not show through it.
	ui.DialogBase(c, &s.open, func(backdrop, panel ui.Element) {
		backdrop.Padding(32).Background(p.scrim)
		panel.Width(480).MaxWidthPercent(100).Radius(22).Border(1, p.hair).
			Shadow(0, 24, 60, 0, p.sheetShadow)
		panel.Children(func() {
			ui.Column(c).Gap(20).Padding(24, 28, 28).Radius(22).Background(p.pop).Children(func() {
				if s.step == 0 {
					a.chooseService(c, p)
				} else {
					a.signIn(c, p)
				}
			})
		})
	})
}

// sheetHead is the sheet's title, with a way back when there is one and the
// close button.
func sheetHead(c *ui.Context, p pal, title, sub string, back func(), close func()) {
	ui.Row(c).AlignItems(ui.Start).Gap(8).Children(func() {
		if back != nil {
			if action.ActionIcon(c, icoBack, action.ActionIconProps{Title: "返回"}).Margin(2, 0, 0, -8).Clicked() {
				back()
			}
		}
		ui.Column(c).Grow(1).MinWidth(0).Gap(4).Padding(4, 0, 0).Children(func() {
			ui.Text(c, title).FontSize(22).FontWeight(600).TextColor(p.ink)
			ui.Text(c, sub).TextColor(p.soft)
		})
		if action.ActionIcon(c, icoClose, action.ActionIconProps{Title: "关闭"}).Margin(2, -8, 0, 0).Clicked() {
			close()
		}
	})
}

func (a *app) chooseService(c *ui.Context, p pal) {
	s := &a.add
	sheetHead(c, p, "添加账户", "选择邮箱服务商，邮件同步后保存在本机。", nil, func() { s.open = false })
	ui.Column(c).Gap(10).Children(func() {
		for i, sv := range services {
			b := ui.ButtonBase(c).FillWidth().MinHeight(72).Gap(14).Padding(0, 16).Radius(16).Justify(ui.Start).
				Border(1, p.readerEdge).Label(sv.name)
			b.Children(func() {
				ui.Box(c).Size(40, 40).Radius(12).Background(p.blue.Alpha(.12)).Center().Children(func() {
					ui.Text(c, sv.tile).FontSize(16).FontWeight(600).TextColor(p.blue)
				})
				ui.Column(c).Grow(1).MinWidth(0).Children(func() {
					ui.Text(c, sv.name).FontSize(15).FontWeight(600).TextColor(p.ink)
					muted(c, p, sv.hint).SingleLine()
				})
				ui.Icon(c, icoChevron).Size(16, 16).TextColor(p.muted)
			})
			if b.Hovered() {
				b.Background(p.selCard).Shadow(0, 6, 18, 0, p.shadow)
			} else {
				b.Background(p.cardHi).Shadow(0, 4, 14, 0, p.shadowSoft)
			}
			if b.Clicked() {
				s.provider, s.step, s.err = i, 1, ""
			}
		}
	})
	muted(c, p, "登录凭据保存在系统钥匙串，agent 无法读取。").TextColor(p.soft)
}

// helpHost is the site of a provider's help page, shown so that the user
// sees where the button leads.
func helpHost(pr mailbox.Provider) string {
	u, _ := url.Parse(pr.Help)
	if u == nil {
		return ""
	}
	return u.Hostname()
}

func (a *app) signIn(c *ui.Context, p pal) {
	s := &a.add
	sv, pr := services[s.provider], mailbox.Providers[s.provider]
	sheetHead(c, p, "登录 "+sv.name, sv.hint, func() { s.step, s.err = 0, "" }, func() { s.open = false })
	label := func(t string) { ui.Text(c, t).FontSize(13).FontWeight(500).TextColor(p.soft) }
	ui.Column(c).Gap(14).Children(func() {
		ui.Column(c).Gap(6).Children(func() {
			label("邮箱地址")
			field.Input(c, &s.email, field.InputProps{Placeholder: "name@example.com", Size: theme.Large}).Label("邮箱地址").AutoFocus()
		})
		ui.Column(c).Gap(6).Children(func() {
			label(pr.Secret)
			field.InputPassword(c, &s.secret, field.InputProps{Placeholder: pr.Secret, Size: theme.Large, Invalid: s.err != ""}).Label(pr.Secret)
		})
		if s.err != "" {
			ui.Text(c, s.err).FontSize(13).TextColor(p.danger)
		}
		// The provider's own page on getting the credential; the browser
		// opens it, and nothing typed above goes with it.
		ui.Column(c).Gap(2).AlignItems(ui.Start).Children(func() {
			if action.Button(c, "如何获取"+pr.Secret, action.ButtonProps{Variant: action.Text}).Margin(0, 0, 0, -12).Clicked() {
				openURL(pr.Help)
			}
			muted(c, p, pr.HelpNote+"（"+helpHost(pr)+"）").MinWidth(0)
		})
	})
	ui.Row(c).AlignItems(ui.Center).Gap(12).Padding(4, 0, 0).Children(func() {
		muted(c, p, "保存在系统钥匙串，agent 无法读取。").TextColor(p.soft).Grow(1).MinWidth(0)
		if s.busy {
			orbSpinner(c, p, connectOrb)
		}
		if action.Button(c, "取消", action.ButtonProps{Size: theme.Large, Variant: action.Text}).Clicked() {
			s.open = false
		}
		text := "连接"
		if s.busy {
			text = "连接中…"
		}
		on := ui.RGB(255, 255, 255)
		if p.dark {
			on = ui.Hex("#0B1020")
		}
		if action.Button(c, text, action.ButtonProps{Size: theme.Large, Variant: action.Primary}).
			Background(p.blue).TextColor(on).Shadow(0, 6, 16, 0, p.blue.Alpha(.3)).Clicked() {
			a.addAccount()
		}
	})
}

// modelStatus tells where the local model stands, and offers to fetch it.
func (a *app) modelStatus(c *ui.Context, p pal) {
	switch {
	case a.dl.busy:
		pct := int64(0)
		if a.dl.total > 0 {
			pct = a.dl.done * 100 / a.dl.total
		}
		muted(c, p, fmt.Sprintf("正在下载 AstrLink Guard… %d%%", pct))
	case a.dir == "":
	case guard.Installed(a.modelFiles()):
		muted(c, p, "AstrLink Guard 已就绪，在本机运行")
	default:
		label := fmt.Sprintf("下载 AstrLink Guard 识别模型（约 %d MB）", guard.Size()>>20+1)
		b := ui.ButtonBase(c).Padding(4, 0).AlignSelf(ui.Start).Label(label)
		b.Children(func() { ui.Text(c, label).FontSize(13).TextColor(p.blue) })
		if b.Clicked() {
			a.downloadModel()
		}
		if a.dl.err != "" {
			ui.Text(c, a.dl.err).FontSize(12).TextColor(p.danger)
		}
	}
}

// The buttons of a sheet follow the system's: at the bottom right, the
// default one last and filled with the accent color, the others beside it
// and to its left; one that cannot act yet is dimmed, not met with
// an error after the click. Escape cancels, as DialogBase has it.

// onAccent is the ink on the accent color.
func onAccent(p pal) ui.Color {
	if p.dark {
		return ui.Hex("#0B1020")
	}
	return ui.RGB(255, 255, 255)
}

// styleSheetButton draws a button of a sheet, with no outline: the default
// button is filled with the accent color, whose edge is that color, and the
// others with a neutral fill. def makes it the default button; one that is
// not enabled is dimmed.
func styleSheetButton(c *ui.Context, p pal, label string, def, enabled bool) ui.Element {
	props := action.ButtonProps{Size: theme.Large, Variant: action.Filled}
	if def {
		props.Variant = action.Primary
	}
	b := action.Button(c, label, props)
	if def {
		bg := p.blue
		switch {
		case !enabled:
		case b.Pressed():
			bg = bg.Mix(ui.RGB(0, 0, 0), .15)
		case b.Hovered():
			bg = bg.Mix(ui.RGB(255, 255, 255), .12)
		}
		b.Background(bg).Border(1, bg).TextColor(onAccent(p))
	}
	if !enabled {
		b.Opacity(.4).AccessDisabled(true)
	}
	return b
}

// sheetButton draws a button of a sheet and reports its click, which a
// button that is not enabled never gives.
func sheetButton(c *ui.Context, p pal, label string, def, enabled bool) bool {
	b := styleSheetButton(c, p, label, def, enabled)
	return enabled && b.Clicked()
}

// disclosure is the arrow and name that unfold what a sheet keeps aside.
func disclosure(c *ui.Context, p pal, label string, open bool) bool {
	ic := icoChevron
	if open {
		ic = icoChevronDown
	}
	b := ui.ButtonBase(c).Height(32).Padding(0, 6).Gap(4).Radius(8).Label(label)
	b.Children(func() {
		ui.Icon(c, ic).Size(14, 14).TextColor(p.soft)
		ui.Text(c, label).FontSize(13).FontWeight(500).TextColor(p.soft)
	})
	if b.Hovered() {
		b.Background(p.hover)
	}
	return b.Clicked()
}

// sheetPanel frames the content of a sheet over the window.
func sheetPanel(c *ui.Context, p pal, open *bool, width float32, gap float32, content func()) {
	ui.DialogBase(c, open, func(backdrop, panel ui.Element) {
		backdrop.Padding(32).Background(p.scrim)
		panel.Width(width).MaxWidthPercent(100).MaxHeightPercent(100).Radius(22).Border(1, p.hair).
			Shadow(0, 24, 60, 0, p.sheetShadow)
		panel.Children(func() {
			ui.Column(c).Gap(gap).Padding(24, 28, 28).Radius(22).Background(p.pop).MaxHeightPercent(100).Children(content)
		})
	})
}

// linkSheet asks before a link clicked in mail opens: mail is read-only, and
// a link may be bait. It is an alert, and the answer that is safe is its
// default button, which Return gives: opening is the one that needs a click.
// It shows the whole address with the site first, as a fake page hides in
// the rest of a long one.
func (a *app) linkSheet(c *ui.Context, p pal) {
	l := &a.link
	if !l.open {
		return
	}
	host := l.url
	if u, err := url.Parse(l.url); err == nil {
		host = u.Hostname()
		if u.Scheme == "mailto" {
			host = u.Opaque
		}
	}
	sheetPanel(c, p, &l.open, 440, 16, func() {
		// Built in the dialog: a shortcut built outside it waits behind it.
		c.OnWindowShortcut(0, ui.KeyEnter, func() { l.open = false })
		ui.Text(c, "要打开这个链接吗？").FontSize(20).FontWeight(600).TextColor(p.ink)
		feedback.Alert(c, "邮件里的链接可能是诱骗点击", feedback.AlertProps{
			Type: feedback.Warning, Description: "例如钓鱼页、伪造的登录页。请确认网站无误。",
		})
		ui.Column(c).Gap(4).Padding(12, 14).Radius(12).Background(p.cardHi).Children(func() {
			ui.Text(c, host).FontSize(16).FontWeight(600).TextColor(p.ink)
			ui.Text(c, l.url).FontSize(12).TextColor(p.soft)
		})
		muted(c, p, "可在统计页关闭“点击邮件里的链接前先确认”。").FontSize(12)
		ui.Row(c).AlignItems(ui.Center).Gap(12).Justify(ui.End).Children(func() {
			if sheetButton(c, p, "仍然打开", false, true) {
				openURL(l.url)
				l.open = false
			}
			// The focus starts on the safe answer, so that Return, which
			// presses the focused button, cannot open the link.
			cancel := styleSheetButton(c, p, "取消", true, true).AutoFocus()
			if cancel.Clicked() {
				l.open = false
			}
		})
	})
	if !l.open {
		l.url = ""
	}
}

// feedbackSheet collects a feedback text, polishes it with the model when
// the user asks, and opens the issue page. Nothing leaves the computer
// before a click: "AI 润色" sends the text to the model, and the default
// button opens the browser. Return is a new line in the text, so ⌘Return
// is the default button's key. Its fields are Lobe's, filled, so that they
// sit on the sheet without a frame of their own.
func (a *app) feedbackSheet(c *ui.Context, p pal) {
	f := &a.fb
	if !f.open {
		return
	}
	cfg := a.llmNow()
	hasText := strings.TrimSpace(f.text) != ""
	sheetPanel(c, p, &f.open, 560, 16, func() {
		c.OnWindowShortcut(ui.Cmd, ui.KeyEnter, func() {
			if hasText {
				a.sendFeedback()
			}
		})
		ui.Column(c).Gap(4).Children(func() {
			ui.Text(c, "反馈问题").FontSize(22).FontWeight(600).TextColor(p.ink)
			ui.Text(c, "写下遇到的问题或想法，也可以让 AI 润色成规范的 issue。").TextColor(p.soft)
		})
		// The body scrolls when the window is too short for it; the buttons stay.
		ui.Scroll(c).Shrink(1).Children(func() {
			ui.Column(c).Gap(16).Children(func() { a.feedbackBody(c, p, cfg, hasText) })
		})
		// What is kept aside is at the bottom left; the buttons at the right.
		ui.Row(c).AlignItems(ui.Center).Gap(12).Padding(4, 0, 0).Children(func() {
			if a.dir != "" {
				if disclosure(c, p, "模型设置", f.set.open) {
					a.toggleLLMForm()
				}
			}
			ui.Box(c).Grow(1)
			if sheetButton(c, p, "取消", false, true) {
				f.open = false
			}
			if sheetButton(c, p, "打开 Issue", true, hasText) {
				a.sendFeedback()
			}
		})
	})
}

func (a *app) feedbackBody(c *ui.Context, p pal, cfg llmSettings, hasText bool) {
	f := &a.fb
	label := func(t string) { ui.Text(c, t).FontSize(13).FontWeight(500).TextColor(p.soft) }
	before := f.text
	field.TextArea(c, &f.text, field.TextAreaProps{Variant: field.InputFilled, Placeholder: "发生了什么？怎么复现？不要贴邮件内容、地址和密码", MinRows: 4, MaxRows: 8}).Label("问题描述")
	if f.text != before {
		f.polished = false // the fields were made from other words
	}
	ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
		text := "AI 润色"
		if f.busy {
			text = "润色中…"
		}
		if sheetButton(c, p, text, false, hasText && !f.busy) {
			a.polishFeedback()
		}
		if f.busy {
			orbSpinner(c, p, connectOrb)
		}
		muted(c, p, "只有点“AI 润色”，上面的文字才会发给"+cfg.who()+"（已隐去邮箱、手机号等）；不点就不会发送。").FontSize(12).Grow(1).MinWidth(0)
	})
	// Who provides the model, and that its free offer may change.
	if cfg.free() {
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			feedback.Alert(c, "润色由 Agnes AI 免费提供", feedback.AlertProps{
				Type: feedback.Info, Description: cfg.Model + " 目前限免，有每分钟请求数限制，以公告为准。",
			}).Grow(1).MinWidth(0)
			if action.Button(c, "查看公告", action.ButtonProps{Variant: action.Link}).TextColor(p.blue).Clicked() {
				openURL(llmNotice)
			}
		})
	} else {
		feedback.Alert(c, "使用你配置的模型服务", feedback.AlertProps{
			Type: feedback.Info, Description: cfg.host() + "，模型 " + cfg.Model,
		})
	}
	if f.err != "" {
		feedback.Alert(c, f.err, feedback.AlertProps{Type: feedback.Error, Variant: feedback.Plain})
	}
	if f.polished {
		ui.Column(c).Gap(6).Children(func() {
			label("标题")
			field.Input(c, &f.draft.Title, field.InputProps{Variant: field.InputFilled, Size: theme.Large}).Label("标题")
			label("现象")
			field.TextArea(c, &f.draft.What, field.TextAreaProps{Variant: field.InputFilled, MinRows: 3, MaxRows: 6}).Label("现象")
			label("期望")
			field.TextArea(c, &f.draft.Expected, field.TextAreaProps{Variant: field.InputFilled, MinRows: 2, MaxRows: 4}).Label("期望")
		})
	}
	if f.set.open {
		a.llmFormView(c, p)
	}
}

// llmFormView is the model settings, as a Lobe form: a label and a note
// beside each field, and the address checked where it is typed. The address,
// the model and the key are those of a service of the user's own; empty ones
// are the built-in.
func (a *app) llmFormView(c *ui.Context, p pal) {
	s := &a.fb.set
	f := s.f
	cfg := a.llmNow()
	keyNote := "留空使用内置免费 key"
	if cfg.KeySource == "custom" {
		keyNote = "已保存你的 key，留空则不变"
	}
	input := func(v *string, ph string, invalid bool) {
		field.Input(c, v, field.InputProps{Variant: field.InputFilled, Placeholder: ph, Size: theme.Large, Invalid: invalid})
	}
	form.View(c, f, form.Props[llmValues]{Variant: form.Borderless, ItemMinWidth: 340}, func() {
		form.Group(c, f, form.GroupProps{}, func() {
			form.Field(c, f, form.FieldProps{Name: "base", Label: "接口地址", Desc: "OpenAI 兼容；换地址会忘记旧 key", Value: f.Values.Base,
				Validate: func() string {
					if v := strings.TrimSpace(f.Values.Base); v != "" {
						if err := checkBase(strings.TrimRight(v, "/")); err != nil {
							return err.Error()
						}
					}
					return ""
				}}, func() { input(&f.Values.Base, "留空使用内置（Agnes AI）", f.Error("base") != "") })
			form.Field(c, f, form.FieldProps{Name: "model", Label: "模型", Value: f.Values.Model}, func() {
				input(&f.Values.Model, defaultModel, false)
			})
			form.Field(c, f, form.FieldProps{Name: "key", Label: "API Key", Desc: keyNote, Value: f.Values.Key}, func() {
				field.InputPassword(c, &f.Values.Key, field.InputProps{Variant: field.InputFilled, Placeholder: "sk-…", Size: theme.Large})
			})
		})
	})
	muted(c, p, "Key 保存在系统钥匙串。命令行：zmail llm set").FontSize(12)
	if s.err != "" {
		feedback.Alert(c, s.err, feedback.AlertProps{Type: feedback.Error, Variant: feedback.Plain})
	}
	ui.Row(c).AlignItems(ui.Center).Gap(8).Justify(ui.End).Children(func() {
		if action.Button(c, "恢复默认", action.ButtonProps{Variant: action.Text}).Clicked() {
			a.resetLLMForm()
		}
		on := f.Error("base") == ""
		b := action.Button(c, "保存", action.ButtonProps{Variant: action.Filled})
		if !on {
			b.Opacity(.4).AccessDisabled(true)
		} else if b.Clicked() {
			a.saveLLM()
		}
	})
}
