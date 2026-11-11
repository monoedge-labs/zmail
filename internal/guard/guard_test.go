package guard

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// With ZMAIL_MODEL_ROOT set, the real model is fetched (once) and read:
// the test that needs 37 MB and the network is left to those who ask for it.
func TestRealModel(t *testing.T) {
	root := t.TempDir()
	if r := os.Getenv("ZMAIL_MODEL_ROOT"); r != "" {
		root = r
	} else {
		t.Skip("set ZMAIL_MODEL_ROOT to fetch and read the real model")
	}
	dir := Dir(root)
	if !Installed(dir) {
		start := time.Now()
		if err := Install(context.Background(), dir, func(done, total int64) {}); err != nil {
			t.Fatal(err)
		}
		t.Logf("installed in %s", time.Since(start))
	}
	start := time.Now()
	m, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("loaded in %s", time.Since(start))

	text := "你好，我是王小明，住在北京市海淀区中关村大街1号，手机 13800138000，邮箱 xiaoming@example.com。Please send it to John Smith at 12 Baker Street, London."
	start = time.Now()
	spans := m.Find(text)
	t.Logf("read %d bytes in %s", len(text), time.Since(start))
	var found []string
	for _, s := range spans {
		found = append(found, s.Label+":"+text[s.Start:s.End])
		t.Logf("%-16s %-24s %.2f", s.Label, text[s.Start:s.End], s.Score)
	}
	all := strings.Join(found, "|")
	for _, want := range []string{"private_person:王小明", "phone:13800138000", "email:xiaoming@example.com"} {
		if !strings.Contains(all, want) {
			t.Errorf("model missed %s in %s", want, all)
		}
	}
}
