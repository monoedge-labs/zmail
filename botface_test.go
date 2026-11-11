package main

import (
	"strings"
	"testing"
	"time"
)

func TestFaceState(t *testing.T) {
	now := time.Now()
	for _, c := range []struct {
		last time.Time
		ops  int
		want string
	}{
		{now.Add(-30 * time.Second), 3, "working"},
		{now.Add(-time.Hour), 3, "default"},
		{now.Add(-time.Hour), 0, "sleeping"},
		{time.Time{}, 0, "sleeping"},
	} {
		if got := faceState(now, c.last, c.ops); got != c.want {
			t.Errorf("faceState(%v, %d) = %q, want %q", now.Sub(c.last).Round(time.Second), c.ops, got, c.want)
		}
	}
}

// The page carries the bundle inside one script element: a closing tag in
// the bundle's strings would end it early.
func TestFacePage(t *testing.T) {
	page := facePage("working")
	if strings.Count(page, "</script>") != 2 {
		t.Errorf("want two script elements, got %d closings", strings.Count(page, "</script>"))
	}
	if !strings.Contains(page, `setAvatar({type:"clover",size:40,state:"working",jumpHeight:8,jumpLean:3})`) {
		t.Error("the page does not set the avatar")
	}
}
