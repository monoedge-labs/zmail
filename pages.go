package main

import (
	"fmt"
	"sort"
	"time"

	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
	"github.com/lobehub/lobe-mygo/pkg/action"
	"github.com/lobehub/lobe-mygo/pkg/choice"
	"github.com/lobehub/lobe-mygo/pkg/feedback"
	"github.com/lobehub/lobe-mygo/pkg/field"
	"github.com/lobehub/lobe-mygo/pkg/theme"

	"github.com/monoedge-labs/zmail/internal/mailbox"
	"github.com/monoedge-labs/zmail/internal/mcpsrv"
)

// page is the frame of the statistics and agent pages: a title with what
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
	"archive_thread": "归档", "move_to_folder": "移到文件夹", "mark_read": "标记已读",
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
				ui.Text(c, "Agent 的操作").FontSize(16).FontWeight(600).TextColor(p.ink)
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

// ---- agent

func maskToken(t string) string {
	if len(t) < 10 {
		return t
	}
	return t[:3] + "••••••••" + t[len(t)-4:]
}

func (a *app) agentPage(c *ui.Context, p pal) {
	side := func() {
		dot, label := p.green, "运行中"
		if !a.mcp.Running() {
			dot, label = p.muted, "已停止"
		}
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Box(c).Size(8, 8).Radius(4).Background(dot).Shadow(0, 0, 0, 3, dot.Alpha(.18))
			ui.Text(c, label).FontWeight(500).TextColor(p.ink)
		})
		btn := "停止服务"
		if !a.mcp.Running() {
			btn = "启动服务"
		}
		if action.Button(c, btn, action.ButtonProps{Size: theme.Large}).Clicked() {
			a.toggleServer()
		}
	}
	page(c, p, "Agent 接入", "外部 agent 通过 MCP 读取和整理邮件，数据留在本机。", side, func() {
		if a.mcpErr != "" {
			feedback.Alert(c, a.mcpErr, feedback.AlertProps{Type: feedback.Error})
		}
		columns(c, 16, func() {
			panel(c, p, p.card).Gap(4).Children(func() {
				ui.Column(c).Gap(2).Padding(0, 0, 8).Children(func() {
					ui.Text(c, "工具权限").FontSize(16).FontWeight(600).TextColor(p.ink)
					muted(c, p, "Agent 只能读取和整理邮件，发送与删除不对外开放。")
				})
				for i, tl := range mcpsrv.Tools {
					ui.Row(c).AlignItems(ui.Center).Gap(12).MinHeight(56).Children(func() {
						ui.Column(c).Grow(1).Children(func() {
							ui.Text(c, tl.Name).Font("Menlo").FontSize(13).FontWeight(500).TextColor(p.ink)
							muted(c, p, tl.Summary)
						})
						on := a.tools[i]
						if choice.Switch(c, &on, choice.SwitchProps{}).Label("允许 " + tl.Name).Changed() {
							a.setTool(i, on)
						}
					})
				}
			})
		}, func() {
			panel(c, p, p.card).Gap(8).Children(func() {
				ui.Text(c, "端点").FontSize(16).FontWeight(600).TextColor(p.ink).Padding(0, 0, 4)
				a.copyRow(c, p, "HTTP", a.endpoint(), a.endpoint(), "")
				a.copyRow(c, p, "命令", "claude mcp add --transport http zmail …", a.claudeCommand(), "")
				a.copyRow(c, p, "令牌", maskToken(a.token), a.token, "重置")
			})
			panel(c, p, p.card).Gap(12).Children(func() {
				ui.Text(c, "已连接").FontSize(16).FontWeight(600).TextColor(p.ink)
				if len(a.clients) == 0 {
					muted(c, p, "还没有 agent 调用")
				}
				names := make([]string, 0, len(a.clients))
				for n := range a.clients {
					names = append(names, n)
				}
				sort.Strings(names)
				for i, n := range names {
					ui.Row(c).AlignItems(ui.Center).Gap(12).Padding(weightPad(i), 0, 0).Children(func() {
						ui.Column(c).Grow(1).Children(func() {
							ui.Text(c, n).Font("Menlo").FontSize(13).FontWeight(500).TextColor(p.ink)
							ui.Text(c, "HTTP").FontSize(12).TextColor(p.muted)
						})
						ui.Text(c, fmt.Sprintf("近 24 小时 %d 次调用", a.clients[n])).FontSize(13).TextColor(p.soft)
					})
				}
			})
			panel(c, p, p.card).Gap(8).Children(func() {
				ui.Text(c, "最近调用").FontSize(16).FontWeight(600).TextColor(p.ink)
				if len(a.ops) == 0 {
					muted(c, p, "还没有调用")
				}
				for _, o := range a.ops {
					ui.Row(c).AlignItems(ui.Center).Gap(12).MinHeight(28).Children(func() {
						ui.Text(c, o.Time.Format("15:04")).FontSize(13).Width(44).TextColor(p.muted)
						ui.Text(c, o.Client+" · "+o.Tool).FontSize(13).SingleLine().Grow(1).TextColor(p.ink)
						st, col := "完成", p.muted
						if !o.OK {
							st, col = "失败", p.danger
						}
						ui.Text(c, st).FontSize(12).TextColor(col)
					})
				}
			})
		})
	})
}

// copyRow is a field of the connection card: what it is, its value in
// monospace, and the buttons that copy it (or, with extra, do that).
func (a *app) copyRow(c *ui.Context, p pal, label, shown, full, extra string) {
	ui.Row(c).AlignItems(ui.Center).Gap(8).MinHeight(48).Padding(0, 4, 0, 14).Radius(12).
		Background(p.field).Border(1, p.fieldEdge).Children(func() {
		ui.Text(c, label).FontSize(12).TextColor(p.muted).Width(40)
		ui.Text(c, shown).Font("Menlo").FontSize(13).TextColor(p.ink).SingleLine().Grow(1).MinWidth(0)
		if extra != "" && action.Button(c, extra, action.ButtonProps{Variant: action.Text}).Clicked() {
			a.resetToken()
		}
		action.CopyButton(c, full, action.CopyButtonProps{})
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
	// The design's sheet: frosted glass over the window, as the window is
	// over the desktop.
	ui.DialogBase(c, &s.open, func(backdrop, panel ui.Element) {
		backdrop.Padding(32).Background(p.scrim)
		panel.Width(480).MaxWidthPercent(100).Radius(22).Border(1, p.sheetEdge).
			Shadow(0, 24, 60, 0, p.sheetShadow).Material(glass.Blur{Radius: 36})
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
	})
	ui.Row(c).AlignItems(ui.Center).Gap(12).Padding(4, 0, 0).Children(func() {
		muted(c, p, "保存在系统钥匙串，agent 无法读取。").TextColor(p.soft).Grow(1).MinWidth(0)
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
