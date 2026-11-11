package mailbox

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/monoedge-labs/zmail/internal/store"
)

// Secrets keeps the passwords of accounts, in the system's keychain.
type Secrets interface {
	Get(accountID string) (string, error)
	Set(accountID, secret string) error
	Delete(accountID string) error
}

// Who did something: the user in the window, or an agent through the CLI.
type Who struct {
	Actor  string // store.ActorMe or store.ActorAgent
	Client string // "ui", or the agent's name
}

var UI = Who{Actor: store.ActorMe, Client: "ui"}

// Service is everything the window and the CLI do with mail. Every
// change goes to the server first and to the local store after it.
type Service struct {
	Store   *store.Store
	Secrets Secrets
	// Dial opens a server for an account; NewIMAP by default.
	Dial func(a store.Account, secret string) Remote
	// Changed is called after anything changed, from any goroutine.
	Changed func()

	mu      sync.Mutex
	remotes map[string]Remote
}

func New(st *store.Store, sec Secrets) *Service {
	return &Service{
		Store: st, Secrets: sec, remotes: map[string]Remote{}, Changed: func() {},
		Dial: dialIMAP,
	}
}

func (s *Service) remote(a store.Account) (Remote, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.remotes[a.ID]; ok {
		return r, nil
	}
	secret, err := s.Secrets.Get(a.ID)
	if err != nil {
		return nil, fmt.Errorf("password of %s: %w", a.Email, err)
	}
	r := s.Dial(a, secret)
	s.remotes[a.ID] = r
	return r, nil
}

func (s *Service) account(id string) (store.Account, error) {
	all, err := s.Store.Accounts()
	if err != nil {
		return store.Account{}, err
	}
	for _, a := range all {
		if a.ID == id {
			return a, nil
		}
	}
	return store.Account{}, store.ErrNotFound
}

// AddAccount checks the login against the server and keeps the account.
func (s *Service) AddAccount(providerID, email, secret string) (store.Account, error) {
	host, err := HostFor(providerID, email)
	if err != nil {
		return store.Account{}, err
	}
	a := store.Account{ID: providerID + ":" + email, Provider: providerID, Email: email, Host: host}
	r := s.Dial(a, secret)
	if _, err := r.Folders(); err != nil {
		r.Close()
		return store.Account{}, err
	}
	if err := s.Secrets.Set(a.ID, secret); err != nil {
		r.Close()
		return store.Account{}, err
	}
	s.mu.Lock()
	s.remotes[a.ID] = r
	s.mu.Unlock()
	if err := s.Store.UpsertAccount(a); err != nil {
		return store.Account{}, err
	}
	s.Changed()
	return a, nil
}

// RemoveAccount forgets an account, its mail and its password here. The
// server's mail stays.
func (s *Service) RemoveAccount(id string) error {
	s.mu.Lock()
	if r, ok := s.remotes[id]; ok {
		r.Close()
		delete(s.remotes, id)
	}
	s.mu.Unlock()
	s.Secrets.Delete(id)
	err := s.Store.DeleteAccount(id)
	s.Changed()
	return err
}

const (
	firstSync = 200 // messages kept per folder when it is first seen
	laterSync = 500
	flagSync  = 200 // newest messages whose read flags are refreshed
)

// SyncAll brings every account up to date; a failing account does not stop
// the others.
func (s *Service) SyncAll() error {
	accs, err := s.Store.Accounts()
	if err != nil {
		return err
	}
	var errs []error
	for _, a := range accs {
		if err := s.SyncAccount(a); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", a.Email, err))
		}
	}
	return errors.Join(errs...)
}

func (s *Service) SyncAccount(a store.Account) error {
	r, err := s.remote(a)
	if err != nil {
		return err
	}
	folders, err := r.Folders()
	if err != nil {
		return err
	}
	var errs []error
	for _, f := range folders {
		if err := s.Store.SetFolder(a.ID, f.Name, f.Role); err != nil {
			return err
		}
	}
	for _, f := range folders {
		if f.Role == RoleSkip {
			continue
		}
		if err := s.syncFolder(a, r, f); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", f.Name, err))
		}
	}
	s.Changed()
	return errors.Join(errs...)
}

func (s *Service) syncFolder(a store.Account, r Remote, f Folder) error {
	last := s.Store.LastUID(a.ID, f.Name)
	limit := firstSync
	if last > 0 {
		limit = laterSync
	}
	hs, err := r.Fetch(f.Name, last, limit)
	if err != nil {
		return err
	}
	for _, h := range hs {
		if err := s.Store.UpsertMessage(store.Message{
			AccountID: a.ID, Folder: f.Name, Role: f.Role, UID: h.UID,
			ThreadKey: ThreadKey(h.Subject), FromName: h.FromName, FromAddr: h.FromAddr,
			Subject: h.Subject, Snippet: Snippet(PlainText(h.Body), 140), Body: h.Body, HTML: h.HTML, Date: h.Date, Seen: h.Seen,
			Category: Categorize(h.FromAddr, h.ListUnsub),
		}); err != nil {
			return err
		}
	}
	if last > 0 {
		if seen, err := r.Flags(f.Name, flagSync); err == nil {
			return s.Store.SetSeenByUID(a.ID, f.Name, seen)
		}
	}
	return nil
}

// Run syncs now and then every interval until ctx ends.
func (s *Service) Run(ctx context.Context, interval time.Duration, onError func(error)) {
	for {
		if err := s.SyncAll(); err != nil && onError != nil {
			onError(err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}

// batch is the messages of one folder of one account: what one server
// command can act on.
type batch struct {
	acc  store.Account
	from string
	msgs []store.Message
}

func (s *Service) batches(ids []int64) ([]batch, error) {
	index := map[string]int{}
	var out []batch
	for _, id := range ids {
		m, err := s.Store.Get(id)
		if err != nil {
			return nil, fmt.Errorf("message %d: %w", id, store.ErrNotFound)
		}
		k := m.AccountID + "\x00" + m.Folder
		i, ok := index[k]
		if !ok {
			a, err := s.account(m.AccountID)
			if err != nil {
				return nil, err
			}
			i = len(out)
			index[k] = i
			out = append(out, batch{acc: a, from: m.Folder})
		}
		out[i].msgs = append(out[i].msgs, m)
	}
	return out, nil
}

func uids(ms []store.Message) []uint32 {
	out := make([]uint32, len(ms))
	for i, m := range ms {
		out[i] = m.UID
	}
	return out
}

func (s *Service) log(who Who, tool, detail string, err error) {
	s.Store.LogOp(store.Op{Actor: who.Actor, Client: who.Client, Tool: tool, Detail: detail, OK: err == nil})
}

// MarkRead sets or clears the read flag of messages.
func (s *Service) MarkRead(who Who, ids []int64, seen bool) (err error) {
	defer func() { s.log(who, "mark_read", fmt.Sprintf("%d messages, seen=%v", len(ids), seen), err) }()
	bs, err := s.batches(ids)
	if err != nil {
		return err
	}
	for _, b := range bs {
		r, err := s.remote(b.acc)
		if err != nil {
			return err
		}
		if err := r.SetSeen(b.from, uids(b.msgs), seen); err != nil {
			return err
		}
		for _, m := range b.msgs {
			if err := s.Store.MarkSeen([]int64{m.ID}, seen, who.Actor); err != nil {
				return err
			}
		}
	}
	s.Changed()
	return nil
}

// Archive files messages in their account's archive folder, creating
// "Archive" where the server has none.
func (s *Service) Archive(who Who, ids []int64) (err error) {
	defer func() { s.log(who, "archive_thread", fmt.Sprintf("%d messages", len(ids)), err) }()
	bs, err := s.batches(ids)
	if err != nil {
		return err
	}
	for _, b := range bs {
		dest := "Archive"
		folders, _ := s.Store.Folders(b.acc.ID)
		for name, role := range folders {
			if role == store.RoleArchive {
				dest = name
				break
			}
		}
		if err := s.move(who, b, dest, store.RoleArchive); err != nil {
			return err
		}
	}
	s.Changed()
	return nil
}

// MoveToFolder files messages in a folder, the junk folder too. Trash and
// drafts are refused: nothing here deletes mail.
func (s *Service) MoveToFolder(who Who, ids []int64, folder string) (err error) {
	defer func() { s.log(who, "move_to_folder", fmt.Sprintf("%d messages to %q", len(ids), folder), err) }()
	bs, err := s.batches(ids)
	if err != nil {
		return err
	}
	for _, b := range bs {
		role := store.RoleOther
		folders, _ := s.Store.Folders(b.acc.ID)
		if r, ok := folders[folder]; ok {
			role = r
		}
		if role == RoleSkip {
			return fmt.Errorf("folder %q is not available", folder)
		}
		if err := s.move(who, b, folder, role); err != nil {
			return err
		}
	}
	s.Changed()
	return nil
}

func (s *Service) move(who Who, b batch, dest, role string) error {
	var todo []store.Message
	for _, m := range b.msgs {
		if m.Folder != dest {
			todo = append(todo, m)
		}
	}
	if len(todo) == 0 {
		return nil
	}
	r, err := s.remote(b.acc)
	if err != nil {
		return err
	}
	mapped, err := r.Move(b.from, uids(todo), dest)
	if err != nil {
		return err
	}
	if err := s.Store.SetFolder(b.acc.ID, dest, role); err != nil {
		return err
	}
	unknown := false
	for _, m := range todo {
		if uid, ok := mapped[m.UID]; ok {
			err = s.Store.Move(m.ID, dest, role, uid, who.Actor)
		} else {
			// The server did not say what UID the message got: forget it
			// here, and read it again from its new folder.
			unknown, err = true, s.Store.DeleteMessage(m.ID)
		}
		if err != nil {
			return err
		}
	}
	if unknown {
		return s.syncFolder(b.acc, r, Folder{Name: dest, Role: role})
	}
	return nil
}
