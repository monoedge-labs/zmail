package mailbox

import (
	"cmp"
	"encoding/json"
	"maps"
	"slices"
	"strings"

	"github.com/monoedge-labs/zmail/internal/store"
)

// CloudAccountsKey is the single key in iCloud KVS holding the JSON array of
// configured account metadata (never passwords or tokens).
const CloudAccountsKey = "accounts"

// KVStore abstracts key-value cloud storage (NSUbiquitousKeyValueStore on macOS).
type KVStore interface {
	Get(key string) (value string, ok bool)
	Set(key, value string) error
	Synchronize() bool
}

type cloudAccount struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	Email    string `json:"email"`
	Host     string `json:"host"`
}

func toCloudAccount(a store.Account) cloudAccount {
	return cloudAccount{ID: a.ID, Provider: a.Provider, Email: a.Email, Host: a.Host}
}

func (c cloudAccount) toStore() store.Account {
	return store.Account{ID: c.ID, Provider: c.Provider, Email: c.Email, Host: c.Host}
}

func (c cloudAccount) valid() bool {
	return c.ID != "" && c.Provider != "" && c.Email != "" && c.Host != ""
}

// MergeCloud reads the account list from Cloud, upserts missing or changed
// accounts into the local store by account ID, and once per machine merges
// pre-existing local accounts back to Cloud. It returns newly added accounts.
func (s *Service) MergeCloud() ([]store.Account, error) {
	if s.Cloud == nil {
		return nil, nil
	}
	changed, added, err := s.mergeCloudLocked()
	if err != nil {
		return nil, err
	}
	if changed && s.Changed != nil {
		s.Changed()
	}
	return added, nil
}

func (s *Service) mergeCloudLocked() (changed bool, added []store.Account, err error) {
	s.cloudMu.Lock()
	defer s.cloudMu.Unlock()

	s.Cloud.Synchronize()
	raw, exists := s.Cloud.Get(CloudAccountsKey)
	var remote []cloudAccount
	if exists && strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &remote); err != nil {
			return false, nil, err
		}
	}

	local, err := s.Store.Accounts()
	if err != nil {
		return false, nil, err
	}
	localByID := make(map[string]store.Account, len(local))
	for _, a := range local {
		localByID[a.ID] = a
	}

	for _, ra := range remote {
		if !ra.valid() {
			continue
		}
		acc := ra.toStore()
		prev, ok := localByID[acc.ID]
		if ok && prev == acc {
			continue
		}
		if err := s.Store.UpsertAccount(acc); err != nil {
			return false, nil, err
		}
		localByID[acc.ID] = acc
		changed = true
		if !ok {
			added = append(added, acc)
		}
	}

	needSeed := !exists || (s.CloudSeeded != nil && !s.CloudSeeded())
	if needSeed {
		merged, err := s.Store.Accounts()
		if err != nil {
			return changed, added, err
		}
		wrote := true
		if len(merged) > 0 && !sameCloudAccounts(remote, merged) {
			if err := s.writeCloudAccounts(merged); err != nil {
				wrote = false
			}
		}
		if wrote && s.MarkCloudSeeded != nil && (exists || len(merged) > 0) {
			s.MarkCloudSeeded()
		}
	}
	return changed, added, nil
}

func (s *Service) pushAccountUpsert(a store.Account) {
	s.pushAccountChange(func(byID map[string]cloudAccount) {
		byID[a.ID] = toCloudAccount(a)
	})
}

func (s *Service) pushAccountRemove(id string) {
	s.pushAccountChange(func(byID map[string]cloudAccount) {
		delete(byID, id)
	})
}

func (s *Service) pushAccountChange(mutate func(byID map[string]cloudAccount)) {
	if s.Cloud == nil {
		return
	}
	s.cloudMu.Lock()
	defer s.cloudMu.Unlock()

	byID := map[string]cloudAccount{}
	if raw, exists := s.Cloud.Get(CloudAccountsKey); exists && strings.TrimSpace(raw) != "" {
		var remote []cloudAccount
		if json.Unmarshal([]byte(raw), &remote) == nil {
			for _, ra := range remote {
				if ra.valid() {
					byID[ra.ID] = ra
				}
			}
		}
	}
	if local, err := s.Store.Accounts(); err == nil {
		for _, a := range local {
			byID[a.ID] = toCloudAccount(a)
		}
	}
	mutate(byID)

	list := make([]store.Account, 0, len(byID))
	for _, ca := range byID {
		if ca.valid() {
			list = append(list, ca.toStore())
		}
	}
	if err := s.writeCloudAccounts(list); err == nil && s.MarkCloudSeeded != nil {
		s.MarkCloudSeeded()
	}
}

func (s *Service) writeCloudAccounts(accounts []store.Account) error {
	payload := encodeCloudAccounts(accounts)
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if err := s.Cloud.Set(CloudAccountsKey, string(b)); err != nil {
		return err
	}
	s.Cloud.Synchronize()
	return nil
}

func encodeCloudAccounts(accounts []store.Account) []cloudAccount {
	out := make([]cloudAccount, 0, len(accounts))
	for _, a := range accounts {
		ca := toCloudAccount(a)
		if ca.valid() {
			out = append(out, ca)
		}
	}
	slices.SortFunc(out, func(x, y cloudAccount) int {
		return cmp.Or(cmp.Compare(x.Email, y.Email), cmp.Compare(x.ID, y.ID))
	})
	return out
}

func sameCloudAccounts(remote []cloudAccount, local []store.Account) bool {
	rm := make(map[string]cloudAccount, len(remote))
	for _, r := range remote {
		if r.valid() {
			rm[r.ID] = r
		}
	}
	lm := make(map[string]cloudAccount, len(local))
	for _, a := range local {
		ca := toCloudAccount(a)
		if ca.valid() {
			lm[ca.ID] = ca
		}
	}
	return maps.Equal(rm, lm)
}
