package main

// data is everything the UI needs from zmail's own packages: the store
// and the service, the secrets, and the inbox queries. UI code calls the
// functions; it never sees the service or its callbacks.

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/egoist/asuka/app"
	"github.com/egoist/asuka/sdk/secure"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"

	"github.com/monoedge-labs/zmail/internal/mailbox"
	"github.com/monoedge-labs/zmail/internal/store"
)

type secrets struct{}

func (secrets) Get(id string) (string, error) {
	if strings.HasPrefix(id, "demo:") {
		return "", nil
	}
	v, ok, err := secure.Get(id)
	if !ok && err == nil {
		err = store.ErrNotFound
	}
	return v, err
}
func (secrets) Set(id, s string) error { return secure.Set(id, s) }
func (secrets) Delete(id string) error { return secure.Delete(id) }

var data struct {
	svc    *mailbox.Service
	st     *store.Store
	cancel context.CancelFunc
}

// openData opens the store in the app's data directory and starts the
// sync loop, once. It runs on a goroutine of the first read's: secrets
// read the keychain and the UI must already be up for it, and no
// lifecycle event promises both. With no accounts it seeds the demo
// mail, so a first run shows something.
var started sync.Once

func openData() {
	started.Do(func() { go open() })
}

func open() error {
	dir, err := app.DataDir()
	if err != nil {
		return err
	}
	st, err := store.Open(filepath.Join(dir, "zmail.db"))
	if err != nil {
		return err
	}
	svc := mailbox.New(st, secrets{})
	dial := svc.Dial
	svc.Dial = func(a store.Account, secret string) mailbox.Remote {
		if a.Provider == "demo" {
			return quiet{}
		}
		return dial(a, secret)
	}
	svc.Changed = func() { ui.Post(func() { tick.Update(func(n int) int { return n + 1 }) }) }
	accs, err := st.Accounts()
	if err != nil {
		return err
	}
	if len(accs) == 0 {
		seedDemo(st)
	}
	data.svc, data.st = svc, st
	ctx, cancel := context.WithCancel(context.Background())
	data.cancel = cancel
	go svc.Run(ctx, time.Minute, nil)
	// The UI may have built while data.st was nil: tell it there's mail.
	ui.Post(func() { tick.Update(func(n int) int { return n + 1 }) })
	return nil
}

func inbox(cat string) []store.Message {
	openData()
	if data.st == nil {
		return nil
	}
	ms, _ := data.st.Search(store.Query{Role: store.RoleInbox, Category: cat, Limit: 300})
	return ms
}

// filterIndex picks a filter segment over the accounts row: 0 is "全部",
// 1..len(providers) are the providers present, the rest are accounts.
var filterIndex = signal.New(0)

// filterProviders is each account's provider name once, in order.
func filterProviders() []string {
	var out []string
	seen := map[string]bool{}
	for _, a := range accounts() {
		name := providerName(a.Provider)
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}

// filterMatch reports whether m passes the current filter: a provider
// segment matches the account ID's prefix, an account segment the ID.
func filterMatch(m store.Message) bool {
	accs := accounts()
	prov := filterProviders()
	i := filterIndex.Get() // Get, not Peek: the list memo tracks it
	switch {
	case i <= 0:
		return true
	case i <= len(prov):
		name := prov[i-1]
		for _, a := range accs {
			if providerName(a.Provider) == name && m.AccountID == a.ID {
				return true
			}
		}
		return false
	case i-len(prov) <= len(accs):
		return m.AccountID == accs[i-len(prov)-1].ID
	}
	return true
}

func message(id int64) store.Message {
	m, _ := data.st.Get(id)
	return m
}

func accounts() []store.Account {
	_ = tick.Get() // re-read when anything changes: callers track it
	openData()
	if data.st == nil {
		return nil
	}
	accs, _ := data.st.Accounts()
	return accs
}

// markRead, archive and addAccount run the server round trips on a
// goroutine; svc.Changed refreshes the UI after.
func markRead(id int64, seen bool) {
	go func() { _ = data.svc.MarkRead(mailbox.UI, []int64{id}, seen) }()
}

func archive(ids ...int64) {
	go func() { _ = data.svc.Archive(mailbox.UI, ids) }()
}

func addAccount(provider, email, secret string) error {
	a, err := data.svc.AddAccount(provider, email, secret)
	if err != nil {
		return err
	}
	go func() { _ = data.svc.SyncAccount(a) }()
	return nil
}

func removeAccount(id string) error { return data.svc.RemoveAccount(id) }

// filterNames is the filter choices: "全部", each provider once, then
// every account's email — the menu pickFilter shows.
func filterNames() []string {
	_ = tick.Get()
	out := []string{"全部"}
	out = append(out, filterProviders()...)
	for _, a := range accounts() {
		out = append(out, a.Email)
	}
	return out
}
