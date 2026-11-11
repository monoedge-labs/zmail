package store

import (
	"slices"
	"strings"
	"testing"
)

func labelStore(t *testing.T) (*Store, []int64) {
	t.Helper()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	var ids []int64
	for uid, role := range []string{RoleInbox, RoleInbox, RoleJunk} {
		m := Message{AccountID: "a", Folder: "INBOX", Role: role, UID: uint32(uid + 1), ThreadKey: "t", Subject: "s"}
		if err := s.UpsertMessage(m); err != nil {
			t.Fatal(err)
		}
	}
	ms, _ := s.Search(Query{Role: "", Folder: "INBOX"})
	for _, m := range ms {
		ids = append(ids, m.ID)
	}
	slices.Sort(ids)
	return s, ids
}

func TestLabels(t *testing.T) {
	s, ids := labelStore(t)
	if err := s.AddLabels(ids[:2], []string{"广告", "待看"}); err != nil {
		t.Fatal(err)
	}
	if err := s.AddLabels(ids[:1], []string{"广告"}); err != nil { // again: no duplicate
		t.Fatal(err)
	}
	if err := s.AddLabels([]int64{ids[2]}, []string{"广告"}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Labels(ids)
	if !slices.Equal(got[ids[0]], []string{"广告", "待看"}) && !slices.Equal(got[ids[0]], []string{"待看", "广告"}) || len(got[ids[0]]) != 2 {
		t.Errorf("labels of the first = %v", got[ids[0]])
	}
	if err := s.AddLabels([]int64{ids[0], 9999}, []string{"x"}); err == nil || !strings.Contains(err.Error(), "9999") {
		t.Errorf("unknown message: err = %v", err)
	}
	if l := (func() []string { g, _ := s.Labels(ids); return g[ids[0]] })(); slices.Contains(l, "x") {
		t.Error("a failed add left a label behind")
	}

	ms, _ := s.Search(Query{Label: "广告"})
	if len(ms) != 2 { // the junk one stays out of searches
		t.Errorf("search by label found %d", len(ms))
	}
	counts, _ := s.LabelCounts()
	if len(counts) != 2 || counts[0] != (LabelCount{"广告", 2}) || counts[1] != (LabelCount{"待看", 2}) {
		t.Errorf("counts = %v", counts)
	}

	if err := s.RemoveLabels(ids[:1], []string{"广告", "不存在"}); err != nil {
		t.Fatal(err)
	}
	if ms, _ := s.Search(Query{Label: "广告"}); len(ms) != 1 {
		t.Errorf("after remove: %d", len(ms))
	}

	// A message gone takes its labels along.
	if err := s.DeleteMessage(ids[1]); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Labels(ids); len(got[ids[1]]) != 0 {
		t.Errorf("a deleted message keeps %v", got[ids[1]])
	}
	if err := s.DeleteAccount("a"); err != nil {
		t.Fatal(err)
	}
	if c, _ := s.LabelCounts(); len(c) != 0 {
		t.Errorf("an account removed leaves %v", c)
	}
}

func TestCleanLabel(t *testing.T) {
	for in, ok := range map[string]bool{"广告": true, "  促销 ": true, "": false, "   ": false, "a\nb": false, strings.Repeat("长", MaxLabelRunes): true, strings.Repeat("长", MaxLabelRunes+1): false} {
		if _, err := CleanLabel(in); (err == nil) != ok {
			t.Errorf("CleanLabel(%q) err = %v", in, err)
		}
	}
}
