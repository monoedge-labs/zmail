package main

import (
	"cmp"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"

	"github.com/monoedge-labs/zmail/internal/store"
)

// prefs are the window's own choices, kept in prefs.json beside the mail
// database. Empty dir keeps them in memory.
type prefs struct {
	// GroupByLabel groups the mail list by label, in place of by category.
	GroupByLabel bool `json:"group_by_label"`
}

func loadPrefs(dir string) (p prefs) {
	if b, err := os.ReadFile(filepath.Join(dir, "prefs.json")); err == nil {
		json.Unmarshal(b, &p)
	}
	return p
}

func (p prefs) save(dir string) error {
	if dir == "" {
		return nil
	}
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "prefs.json"), b, 0o600)
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
