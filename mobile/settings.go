package main

// The Settings tab: the accounts and the add-account sheet.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/asuka/colors"
	"github.com/egoist/asuka/fonts"
	"github.com/egoist/asuka/sdk/webauth"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"

	"github.com/monoedge-labs/zmail/internal/mailbox"
)

var adding = signal.New(false)

// useOAuth is the Google sheet's sign-in mode; it defaults to OAuth (a
// Workspace account cannot use a password at all) and stays off for every
// other provider: a stale OAuth flag must not wave a password field
// through.
var useOAuth = signal.New(true)

// The add-account sheet's fields and state, package-level so googleSignIn
// can update them once the OAuth sheet closes.
var (
	addEmail    = signal.New("")
	addSecret   = signal.New("")
	addBusy     = signal.New(false)
	addErr      = signal.New("")
	addProvider = signal.New(0)
)

type accountRow struct {
	ID       string
	Email    string
	Provider string
	add      bool // the "Add account" row
}

func settingsStack() ui.Node {
	return ui.Stack(func() ui.Node { return settings() }, ui.ScreenOptions{
		Title: ui.Val("设置"),
	})
}

func settings() ui.Node {
	rows := signal.Func(func() []accountRow {
		_ = tick.Get()
		var out []accountRow
		for _, a := range accounts() {
			out = append(out, accountRow{ID: a.ID, Email: a.Email, Provider: a.Provider})
		}
		return append(out, accountRow{add: true})
	})
	return ui.View(ui.ViewProps{Style: ui.Style{Grow: 1}},
		ui.List(ui.ListProps{
			Appearance: ui.ListInsetGrouped,
			SwipeActions: func(i int) []ui.Action {
				all := rows.Get()
				if i < 0 || i >= len(all) {
					return nil
				}
				r := all[i]
				if r.add {
					return nil
				}
				return []ui.Action{{Title: ui.Val("删除"), Icon: ui.IconTrash, Role: ui.ActionDestructive,
					OnPress: func() {
						ui.Alert(ui.AlertProps{Title: "移除账户？",
							Message: r.Email + " 及其本地邮件将从这台设备删除，服务器上的邮件不受影响。",
							Actions: []ui.Action{
								{Title: ui.Val("取消"), Role: ui.ActionCancel},
								{Title: ui.Val("移除"), Role: ui.ActionDestructive, OnPress: func() {
									go func() { _ = removeAccount(r.ID) }()
								}},
							},
						})
					}}}
			},
		},
			rows, func(r accountRow) string { return r.ID },
			func(item signal.Accessor[accountRow], _ signal.Accessor[int]) ui.Node {
				if signal.Untrack(item.Get).add {
					return ui.View(ui.ViewProps{
						OnPress: func() { adding.Set(true) },
						Style:   ui.Style{Padding: ui.Symmetric(12, 16), Direction: ui.RowDir, Gap: 8, Align: ui.AlignCenter},
					},
						ui.Text(ui.Val("＋"), ui.Style{Color: colors.Accent, Font: fonts.Headline}),
						ui.StaticText("添加账户", ui.Style{Color: colors.Accent}),
					)
				}
				return ui.View(ui.ViewProps{
					Style: ui.Style{Padding: ui.Symmetric(10, 16), Gap: 2},
				},
					ui.Text(signal.Func(func() string { return item.Get().Email }), ui.Style{Lines: 1}),
					ui.Text(signal.Func(func() string { return providerName(item.Get().Provider) }),
						ui.Style{Font: fonts.Footnote, Color: colors.SecondaryLabel}),
				)
			}),
		ui.Sheet(ui.SheetProps{Open: adding, Grabber: true, Detents: ui.DetentMedium | ui.DetentLarge,
			OnDismiss: func() {
				// A fresh sheet starts on Google with OAuth on and nothing typed.
				addProvider.Set(0)
				useOAuth.Set(true)
				addEmail.Set("")
				addSecret.Set("")
				addErr.Set("")
				addBusy.Set(false)
			},
			Content: func() ui.Node { return addSheet() }}),
	)
}

func providerName(id string) string {
	if p, ok := mailbox.ProviderByID(id); ok {
		return p.Name
	}
	if id == "demo" {
		return "演示账户"
	}
	return id
}

// addSheet adds one IMAP account: a provider, an address, and the
// credential the provider's page makes (app password or code, never the
// mail password).
func addSheet() ui.Node {
	provider := addProvider
	names := signal.Func(func() []string {
		var out []string
		for _, p := range mailbox.Providers {
			out = append(out, p.Name)
		}
		return out
	})
	add := func() {
		addBusy.Set(true)
		addErr.Set("")
		p := mailbox.Providers[provider.Peek()]
		e, s := strings.TrimSpace(addEmail.Peek()), addSecret.Peek()
		if p.ID == "google" && useOAuth.Peek() {
			googleSignIn(e)
			return
		}
		go func() {
			err := addAccount(p.ID, e, s)
			ui.Post(func() {
				addBusy.Set(false)
				if err != nil {
					addErr.Set(fmt.Sprintf("verify %q pass %d oauth=%v: %v", e, len(s), useOAuth.Peek(), err))
				} else {
					addEmail.Set("")
					addSecret.Set("")
					adding.Set(false)
				}
			})
		}()
	}
	// A provider that is not Google needs a password; a stale OAuth flag
	// from the last Google try must not wave it through.
	signal.RenderEffect(func() {
		if !isGoogle(provider.Get()) {
			useOAuth.Set(false)
		}
	})
	line := func(label string, v *signal.Signal[string], kb ui.KeyboardType, secureField bool) ui.Node {
		return ui.Row(ui.Style{Gap: 8, Align: ui.AlignCenter, Padding: ui.Symmetric(8, 20)},
			ui.StaticText(label, ui.Style{Color: colors.SecondaryLabel, Width: ui.Pt(60)}),
			ui.TextField(ui.TextFieldProps{
				Value: v, Plain: true, Keyboard: kb, Secure: secureField,
				Autocapitalize: ui.CapitalizeNone, Autocorrect: ui.AutocorrectNo,
				Style: ui.Style{Grow: 1},
			}),
		)
	}
	return ui.SafeArea(ui.Style{Grow: 1},
		ui.Row(ui.Style{Justify: ui.JustifySpaceBetween, Align: ui.AlignCenter, Padding: ui.Symmetric(8, 20)},
			ui.Button(ui.ButtonProps{Title: ui.Val("取消"), OnPress: func() { adding.Set(false) }}),
			ui.StaticText("添加账户", ui.Style{Font: fonts.Headline}),
			ui.Button(ui.ButtonProps{Title: ui.Val("添加"), Variant: ui.ButtonFilled,
				Disabled: signal.Func(func() bool {
					return addBusy.Get() || strings.TrimSpace(addEmail.Get()) == "" || !(isGoogle(provider.Get()) && useOAuth.Get()) && addSecret.Get() == ""
				}),
				OnPress: add}),
		),
		ui.Row(ui.Style{Gap: 8, Align: ui.AlignCenter, Padding: ui.Symmetric(8, 20)},
			ui.StaticText("服务商", ui.Style{Color: colors.SecondaryLabel, Width: ui.Pt(60)}),
			ui.Picker(ui.PickerProps{Items: names, Selected: provider, Label: ui.Val("选择")}),
		),
		line("邮箱", addEmail, ui.KeyboardEmail, false),
		ui.Show(signal.Func(func() bool { return isGoogle(provider.Get()) }),
			func() ui.Node {
				return ui.Row(ui.Style{Gap: 8, Align: ui.AlignCenter, Padding: ui.Symmetric(8, 20)},
					ui.Toggle(ui.ToggleProps{Value: useOAuth}),
					ui.StaticText("OAuth 登录", ui.Style{Font: fonts.Body}),
					ui.Text(signal.Func(func() string {
						if useOAuth.Get() {
							return "浏览器登录（Workspace 账号推荐）"
						}
						return "使用应用专用密码"
					}), ui.Style{Font: fonts.Footnote, Color: colors.SecondaryLabel}),
				)
			}),
		ui.Show(signal.Func(func() bool { return !(isGoogle(provider.Get()) && useOAuth.Get()) }),
			func() ui.Node {
				return line("密码", addSecret, ui.KeyboardDefault, true)
			}),
		ui.Text(signal.Func(func() string {
			p := mailbox.Providers[provider.Get()]
			if isGoogle(provider.Get()) && useOAuth.Get() {
				return "点添加会打开 Google 登录页，授权后自动完成。"
			}
			return p.HelpNote + "（" + p.Secret + "）"
		}), ui.Style{Font: fonts.Footnote, Color: colors.SecondaryLabel, Padding: ui.Symmetric(4, 20)}),
		ui.Button(ui.ButtonProps{Title: ui.Val("查看获取方法"), Style: ui.Style{Margin: ui.Symmetric(0, 20)},
			OnPress: func() { ui.OpenURL(mailbox.Providers[provider.Peek()].Help) }}),
		ui.Text(addErr, ui.Style{Font: fonts.Footnote, Color: colors.Accent, Padding: ui.Symmetric(8, 20)}),
	)
}

func isGoogle(i int) bool { return mailbox.Providers[i].ID == "google" }

// googleSignIn runs the Google OAuth flow in the system's sign-in sheet
// (ASWebAuthenticationSession): the mailbox package's code flow needs a
// browser it can show and a loopback answer.
func googleSignIn(email string) {
	addBusy.Set(true)
	addErr.Set("")
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		s, signed, err := mailbox.GoogleLogin(ctx, mailbox.GoogleClientID, mailbox.GoogleClientSecret, email,
			func(url string) {
				ui.Post(func() { openAuth(url) })
			})
		ui.Post(func() {
			addBusy.Set(false)
			if err != nil {
				addErr.Set(err.Error())
				return
			}
			if signed != "" {
				email = signed
			}
			go func() {
				err := addAccount("google", email, s)
				ui.Post(func() {
					if err != nil {
						addErr.Set(err.Error())
					} else {
						addEmail.Set("")
						addSecret.Set("")
						adding.Set(false)
					}
				})
			}()
		})
	}()
}

// openAuth starts the sign-in sheet on the URL. The login page redirects
// to the loopback server GoogleLogin runs; webauth's session ends there
// (the app cancels it), so no callback scheme is needed.
func openAuth(url string) {
	webauth.Start(webauth.Options{URL: url}, func(_ string, err error) {
		if err != nil && !errors.Is(err, webauth.ErrCanceled) {
			addErr.Set(err.Error())
			addBusy.Set(false)
		}
	})
}
