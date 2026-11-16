package main

// Zmail on a phone: tabs for the inbox's categories, a message screen,
// accounts. The mail itself is zmail's store and mailbox; this file is
// only its UI, in asuka.

import (
	"strconv"
	"strings"
	"time"

	"github.com/egoist/asuka/app"
	"github.com/egoist/asuka/colors"
	"github.com/egoist/asuka/fonts"
	"github.com/egoist/asuka/sdk/markdown"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"

	"github.com/monoedge-labs/zmail/internal/mailbox"
	"github.com/monoedge-labs/zmail/internal/store"
)

// tick bumps after every store change (sync, swipe, account); the lists
// reload on it.
var tick = signal.New(0)

func main() { app.Run(Root) }

func Root() ui.Node {
	badge := func(cat string) signal.Accessor[string] {
		return signal.Func(func() string {
			_ = tick.Get()
			n := 0
			for _, m := range inbox(cat) {
				if !m.Seen {
					n++
				}
			}
			return strconv.Itoa(n)
		})
	}
	tabs := []ui.Tab{
		{Title: ui.Val("人物"), Icon: ui.IconPerson, Badge: badge(store.CatPerson),
			Content: func() ui.Node { return listStack(store.CatPerson, "人物") }},
		{Title: ui.Val("通知"), Icon: ui.IconBell, Badge: badge(store.CatNotification),
			Content: func() ui.Node { return listStack(store.CatNotification, "通知") }},
		{Title: ui.Val("订阅"), Icon: "newspaper", Badge: badge(store.CatNewsletter),
			Content: func() ui.Node { return listStack(store.CatNewsletter, "订阅") }},
		{Title: ui.Val("设置"), Icon: ui.IconSettings,
			Content: func() ui.Node { return settingsStack() }},
	}
	return ui.Tabs(ui.TabsProps{}, tabs...)
}

func listStack(cat, title string) ui.Node {
	screenTitle := signal.Func(func() string {
		if i := filterIndex.Get(); i > 0 {
			if ns := filterNames(); i < len(ns) {
				return title + " · " + ns[i]
			}
		}
		return title
	})
	return ui.Stack(func() ui.Node { return mailList(cat) }, ui.ScreenOptions{
		Title:  screenTitle,
		Search: &ui.Search{Text: searchText},
		Actions: []ui.Action{
			{Title: ui.Val("筛选"), Icon: ui.IconFilter, OnPress: pickFilter},
			{Title: ui.Val("添加账户"), Icon: ui.IconPlus, OnPress: func() { adding.Set(true) }},
		},
	})
}

// pickFilter opens an action sheet over "全部 | provider | each account".
// An Action's menu children are fixed at build time, so a new account
// wouldn't reach them; building an Alert on press reads them fresh.
func pickFilter() {
	ns := filterNames()
	cur := filterIndex.Peek()
	acts := make([]ui.Action, 0, len(ns)+1)
	for i, name := range ns {
		i := i
		t := name
		if i == cur {
			t = "✓ " + name
		}
		acts = append(acts, ui.Action{Title: ui.Val(t), OnPress: func() { filterIndex.Set(i) }})
	}
	acts = append(acts, ui.Action{Title: ui.Val("取消"), Role: ui.ActionCancel})
	ui.Alert(ui.AlertProps{Title: "筛选", Actions: acts, ActionSheet: true})
}

var searchText = signal.New("")

func messages(cat string) []store.Message {
	_ = tick.Get()
	ms := inbox(cat)
	if i := filterIndex.Get(); i > 0 {
		out := make([]store.Message, 0, len(ms))
		for _, m := range ms {
			if filterMatch(m) {
				out = append(out, m)
			}
		}
		ms = out
	}
	q := strings.TrimSpace(searchText.Get())
	if q == "" {
		return ms
	}
	out := make([]store.Message, 0, len(ms))
	low := strings.ToLower(q)
	for _, m := range ms {
		if strings.Contains(strings.ToLower(m.Subject+" "+m.FromName+" "+m.FromAddr), low) {
			out = append(out, m)
		}
	}
	return out
}

func day(t time.Time) string {
	y, m, d := t.Date()
	ny, nm, nd := time.Now().Date()
	switch {
	case y == ny && m == nm && d == nd:
		return "今天"
	case y == ny && m == nm && d == nd-1:
		return "昨天"
	}
	return t.Format("1月2日 周一")
}

func mailList(cat string) ui.Node {
	nav := ui.UseNavigator()
	refreshing := signal.New(false)
	ms := signal.Func(func() []store.Message { return messages(cat) })
	// at reads row i while the memo still has it: the host may ask for a
	// row the last update just removed (a search clearing the list), so
	// an out-of-range index gives back a zero message, not a crash.
	at := func(i int) store.Message {
		if list := ms.Get(); i >= 0 && i < len(list) {
			return list[i]
		}
		return store.Message{}
	}
	list := func() ui.Node {
		return ui.List(ui.ListProps{
			Appearance: ui.ListInsetGrouped,
			Refreshing: refreshing,
			OnRefresh: func() {
				refreshing.Set(true)
				go func() {
					_ = data.svc.SyncAll()
					ui.Post(func() { refreshing.Set(false) })
				}()
			},
			Section: func(i int) string {
				if m := at(i); m.ID != 0 {
					return day(m.Date)
				}
				return ""
			},
			Header: func(d signal.Accessor[string]) ui.Node {
				return ui.View(ui.ViewProps{Style: ui.Style{Padding: ui.Symmetric(6, 16)}},
					ui.Text(d, ui.Style{Font: fonts.Footnote.Weighted(fonts.Semibold), Color: colors.SecondaryLabel}))
			},
			LeadingSwipeActions: func(i int) []ui.Action {
				m := at(i)
				if m.ID == 0 {
					return nil
				}
				title := "标为未读"
				if m.Seen {
					title = "标为已读"
				}
				return []ui.Action{{Title: ui.Val(title), Icon: ui.IconMail, OnPress: func() { markRead(m.ID, !m.Seen) }}}
			},
			SwipeActions: func(i int) []ui.Action {
				id := at(i).ID
				if id == 0 {
					return nil
				}
				return []ui.Action{{Title: ui.Val("归档"), Icon: "archivebox", OnPress: func() { archive(id) }}}
			},
		}, ms, func(m store.Message) int64 { return m.ID },
			func(item signal.Accessor[store.Message], _ signal.Accessor[int]) ui.Node {
				return ui.View(ui.ViewProps{
					OnPress: func() {
						m := item.Get()
						if !m.Seen {
							markRead(m.ID, true)
						}
						nav.Push(func() ui.Node { return messageScreen(m.ID) }, ui.ScreenOptions{Title: ui.Val(who(m))})
					},
					Style: ui.Style{Direction: ui.RowDir, Gap: 10, Padding: ui.Symmetric(10, 12), Align: ui.AlignStart},
				},
					// The unread dot: accent while unread, its room kept when read.
					ui.View(ui.ViewProps{Style: ui.StyleFunc(func() ui.Style {
						s := ui.Style{Width: ui.Pt(8), Height: ui.Pt(8), CornerRadius: 4, Margin: ui.Edges{Top: 15}}
						if !item.Get().Seen {
							s.Background = colors.Accent
						}
						return s
					})}),
					// The sender's initial in a circle, as the desktop's avatar.
					ui.View(ui.ViewProps{Style: ui.Style{Width: ui.Pt(36), Height: ui.Pt(36), CornerRadius: 18,
						Background: colors.SecondaryGroupedBackground, Justify: ui.JustifyCenter, Align: ui.AlignCenter, Shrink: 0}},
						ui.Text(signal.Func(func() string { return initial(who(item.Get())) }),
							ui.Style{Font: fonts.System(15).Weighted(fonts.Semibold), Color: colors.Label}),
					),
					ui.Column(ui.Style{Grow: 1, Shrink: 1, Gap: 2},
						ui.Row(ui.Style{Justify: ui.JustifySpaceBetween, Gap: 8},
							ui.Text(signal.Func(func() string { return who(item.Get()) }),
								ui.StyleFunc(func() ui.Style {
									return ui.Style{Font: fonts.Body.Weighted(weight(!item.Get().Seen)), Lines: 1, Shrink: 1,
										Color: ink(item.Get().Seen)}
								})),
							ui.Text(signal.Func(func() string { return shortDate(item.Get().Date) }),
								ui.Style{Font: fonts.Footnote, Color: colors.SecondaryLabel}),
						),
						ui.Text(signal.Func(func() string { return item.Get().Subject }),
							ui.StyleFunc(func() ui.Style {
								return ui.Style{Font: fonts.Body.Weighted(weight(!item.Get().Seen)), Lines: 1, Color: ink(item.Get().Seen)}
							})),
						ui.Text(signal.Func(func() string { return item.Get().Snippet }),
							ui.Style{Font: fonts.Subheadline, Color: colors.SecondaryLabel, Lines: 2}),
					),
				)
			})
	}
	return list()
}

// who, initial, ink and weight shape a row as the desktop's (view.go,
// theme.go): the sender's name or address, its first letter, the ink
// strongest while unread, the weight heavier then.
func who(m store.Message) string {
	if m.FromName != "" {
		return m.FromName
	}
	return m.FromAddr
}

func initial(s string) string {
	for _, r := range s {
		return string(r)
	}
	return "?"
}

func ink(seen bool) ui.Color {
	if seen {
		return colors.SecondaryLabel
	}
	return colors.Label
}

func weight(unread bool) fonts.Weight {
	if unread {
		return fonts.Bold
	}
	return fonts.Medium
}

func shortDate(t time.Time) string {
	if time.Since(t) < 24*time.Hour {
		return t.Format("15:04")
	}
	return t.Format("1/2")
}

func messageScreen(id int64) ui.Node {
	m := signal.New(message(id))
	header := ui.SafeArea(ui.Style{Padding: ui.Edges{Top: 20, Left: 20, Right: 20}, Gap: 10},
		ui.Text(signal.Func(func() string { return m.Get().Subject }), ui.Style{Font: fonts.Title2}),
		ui.Row(ui.Style{Gap: 8, Align: ui.AlignCenter},
			ui.Text(signal.Func(func() string { return who(m.Get()) + " <" + m.Get().FromAddr + ">" }),
				ui.Style{Font: fonts.Subheadline, Color: colors.SecondaryLabel, Shrink: 1, Lines: 1}),
			ui.Text(signal.Func(func() string { return m.Get().Date.Format("1月2日 15:04") }),
				ui.Style{Font: fonts.Footnote, Color: colors.SecondaryLabel}),
		),
		ui.View(ui.ViewProps{Style: ui.Style{Height: ui.Pt(1), Background: colors.Separator}}),
	)
	body := signal.Func(func() string { return mailbox.PageHTML(m.Get().HTML, "") })
	return ui.View(ui.ViewProps{Style: ui.Style{Grow: 1, Background: colors.Background}},
		header,
		ui.Show(signal.Func(func() bool { return m.Get().HTML != "" }),
			func() ui.Node {
				return ui.WebView(ui.WebViewProps{HTML: body, Style: ui.Style{Grow: 1}})
			},
			func() ui.Node {
				return ui.Scroll(ui.ScrollProps{Style: ui.Style{Grow: 1}},
					ui.View(ui.ViewProps{Style: ui.Style{Padding: ui.Symmetric(0, 20)}},
						markdown.View(markdown.Props{Text: signal.Func(func() string {
							b := m.Get().Body
							if b == "" {
								b = m.Get().Snippet
							}
							return b
						})}),
					),
				)
			}),
	)
}
