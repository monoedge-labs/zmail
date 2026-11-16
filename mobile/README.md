# Zmail mobile (asuka)

The phone app, in [asuka](https://github.com/egoist/asuka). It reuses
zmail's `internal/store` and `internal/mailbox` (the SQLite store, the
IMAP sync and the account providers) and only re-does the UI; secrets go
to the platform's secure storage, not the macOS keychain.

First run with no accounts seeds the same demo mail as the desktop's
`-demo`, under a `demo:` account that syncs nowhere.

## Run

```sh
go run github.com/egoist/asuka/cmd/asuka dev -p mac mobile   # as a window on this Mac
go run github.com/egoist/asuka/cmd/asuka dev -p ios mobile   # the simulator
go run github.com/egoist/asuka/cmd/asuka dev -p android mobile
```

`asuka dev` watches the sources and reloads on save.

## What's here

- Tabs for the inbox's categories (人物 / 通知 / 订阅) with unread
  badges, day sections, pull to refresh, swipe read/unread and archive,
  and a search bar.
- A message screen rendering the stored Markdown body.
- A settings tab: the accounts and a sheet to add one (provider picker,
  email, credential, and the provider's help page).

## Not yet

- Real-phone verification (only the macOS host was run).
- Threads (a message shows alone), folders other than inbox/archive,
  and the window's extras (labels, the agent log, notifications).
