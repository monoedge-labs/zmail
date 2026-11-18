package main

import (
	"cmp"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/monoedge-labs/zmail/internal/mailbox"
	"github.com/monoedge-labs/zmail/internal/store"
)

// prefs are the window's own choices, kept in prefs.json beside the mail
// database. Empty dir keeps them in memory.
type prefs struct {
	// GroupByLabel groups the mail list by label, in place of by category.
	GroupByLabel bool `json:"group_by_label"`
	// NotifyOff turns the desktop notice of new mail off. Notices are on by
	// default: the first one asks macOS for permission anyway.
	NotifyOff bool `json:"notify_off"`
	// ICloudSeeded records that this Mac has merged its local accounts into
	// iCloud KVS once.
	ICloudSeeded bool `json:"icloud_seeded,omitempty"`
}

var prefsMu sync.Mutex

func loadPrefs(dir string) (p prefs) {
	prefsMu.Lock()
	defer prefsMu.Unlock()
	return loadPrefsLocked(dir)
}

func loadPrefsLocked(dir string) (p prefs) {
	if dir == "" {
		return p
	}
	if b, err := os.ReadFile(filepath.Join(dir, "prefs.json")); err == nil {
		json.Unmarshal(b, &p)
	}
	return p
}

func (p prefs) save(dir string) error {
	if dir == "" {
		return nil
	}
	prefsMu.Lock()
	defer prefsMu.Unlock()
	if !p.ICloudSeeded && loadPrefsLocked(dir).ICloudSeeded {
		p.ICloudSeeded = true
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "prefs.json"), b, 0o600)
}

// wireCloud attaches iCloud KVS account sync to svc when the binary has the
// ubiquity kvstore entitlement, backing its one-time seed flag in prefs.json.
func wireCloud(svc *mailbox.Service, dir string, onChange func()) {
	svc.Cloud = openICloudKVS(onChange)
	if svc.Cloud == nil || dir == "" {
		return
	}
	svc.CloudSeeded = func() bool {
		return loadPrefs(dir).ICloudSeeded
	}
	svc.MarkCloudSeeded = func() {
		prefsMu.Lock()
		defer prefsMu.Unlock()
		p := loadPrefsLocked(dir)
		if p.ICloudSeeded {
			return
		}
		p.ICloudSeeded = true
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return
		}
		if b, err := json.Marshal(p); err == nil {
			_ = os.WriteFile(filepath.Join(dir, "prefs.json"), b, 0o600)
		}
	}
}

// labelGroup is the rows (by index) that carry a label; Label "" holds the
// rows with none.
type labelGroup struct {
	Label string
	Rows  []int
}

// labelGroups groups rows by label: the biggest group first, equal ones by
// name, and the unlabeled last. A row with several labels is in each group.
func labelGroups(rows []store.Message, labels map[int64][]string) []labelGroup {
	by := map[string][]int{}
	var none []int
	for i, m := range rows {
		if len(labels[m.ID]) == 0 {
			none = append(none, i)
		}
		for _, l := range labels[m.ID] {
			by[l] = append(by[l], i)
		}
	}
	var out []labelGroup
	for l, idx := range by {
		out = append(out, labelGroup{l, idx})
	}
	slices.SortFunc(out, func(x, y labelGroup) int {
		return cmp.Or(len(y.Rows)-len(x.Rows), cmp.Compare(x.Label, y.Label))
	})
	if len(none) > 0 {
		out = append(out, labelGroup{"", none})
	}
	return out
}
